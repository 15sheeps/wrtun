package socks5

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/15sheeps/wrtun/pkg/wrtc"
	"github.com/pion/webrtc/v4"
	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/bufferpool"
)

const copyBufferSize = 262144

type Socks5Adapter struct {
	*slog.Logger
}

func (l Socks5Adapter) Errorf(format string, args ...interface{}) {
	l.Error(format, args...)
}

type Tunnel struct {
	wrtc.Config
}

func NewTunnel(
	transport wrtc.Transport,
	iceProvider wrtc.Provider,
	optLogger ...*slog.Logger,
) (tun *Tunnel) {
	var l *slog.Logger
	if len(optLogger) > 0 {
		l = optLogger[0]
	} else {
		l = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
	}
	tun = &Tunnel{
		Config: wrtc.Config{
			Transport: transport,
			Provider:  iceProvider,
			Logger:    l,
		},
	}
	return
}

// ---------------------------------------------------------------------------
// ICE / RTT stats logger
// ---------------------------------------------------------------------------

// logPCStats starts a background goroutine that prints the selected ICE
// candidate pair (host/srflx/relay) and the current round-trip time every
// 5 s until ctx is cancelled.  This tells us immediately whether traffic is
// going through a TURN relay and what the path RTT is — both are critical for
// diagnosing the throughput formula:  throughput ≈ recv_window / RTT.
func logPCStats(ctx context.Context, pc *webrtc.PeerConnection, log *slog.Logger) {
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				report := pc.GetStats()
				for _, s := range report {
					pair, ok := s.(webrtc.ICECandidatePairStats)
					if !ok || !pair.Nominated {
						continue
					}
					rttMs := pair.CurrentRoundTripTime * 1000
					log.Info("ICE stats",
						"rtt_ms", fmt.Sprintf("%.1f", rttMs),
						"local_candidate", pair.LocalCandidateID,
						"remote_candidate", pair.RemoteCandidateID,
						"bytes_sent", pair.BytesSent,
						"bytes_received", pair.BytesReceived,
					)

					// Also print the candidate types so we know if TURN is in the path.
					for _, cs := range report {
						cand, ok := cs.(webrtc.ICECandidateStats)
						if !ok {
							continue
						}
						if cand.ID == pair.LocalCandidateID || cand.ID == pair.RemoteCandidateID {
							log.Info("ICE candidate",
								"id", cand.ID,
								"type", cand.CandidateType.String(),
								"protocol", cand.Protocol,
								"address", cand.IP,
								"port", cand.Port,
							)
						}
					}
					break // only the nominated pair matters
				}
			}
		}
	}()
}

// bindConnections copies data bidirectionally between a TCP connection and a
// WebRTC DataChannel.
func bindConnections(tcp net.Conn, dc *wrtc.DataChannel, connID int, log *slog.Logger) error {
	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	setErr := func(err error) { errOnce.Do(func() { firstErr = err }) }

	// tcp -> datachannel
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, copyBufferSize)
		n, err := io.CopyBuffer(dc, tcp, buf)

		log.Debug("tcp->dc copy finished", "conn_id", connID, "bytes", n, "err", err)
		if err != nil {
			setErr(fmt.Errorf("tcp->dc: %w", err))
		}
		_ = dc.Close()
	}()

	// datachannel -> tcp
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, copyBufferSize)
		n, err := io.CopyBuffer(tcp, dc, buf)

		log.Debug("dc->tcp copy finished", "conn_id", connID, "bytes", n, "err", err)
		if err != nil {
			setErr(fmt.Errorf("dc->tcp: %w", err))
		}
		_ = tcp.Close()
		_ = dc.Close()
	}()

	wg.Wait()
	return firstErr
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

func (tun *Tunnel) startServer(ctx context.Context) error {
	tun.Info("starting peer listener")

	ncfg := tun.Config
	ncfg.Logger = tun.Config.Logger.With("where", "wrtc")
	peerListener, err := wrtc.ListenPeerConnections(ctx, ncfg)
	if err != nil {
		return fmt.Errorf("failed to listen peer connections: %w", err)
	}
	defer peerListener.Close()

	server := socks5.NewServer(
		socks5.WithLogger(Socks5Adapter{Logger: tun.Config.Logger}),
		socks5.WithBufferPool(bufferpool.NewPool(1024*1024)),
	)

	for {
		tun.Debug("waiting for incoming peer connection...")

		peerConn, err := peerListener.Accept()
		if err != nil {
			tun.Error("error accepting peer connection", "err", err)
			continue
		}

		tun.Info("accepted new peer connection")

		go func() {
			// Log ICE/RTT stats for this peer connection.
			statsCtx, cancelStats := context.WithCancel(ctx)
			logPCStats(statsCtx, peerConn, tun.Config.Logger)
			defer cancelStats()

			dcListener := wrtc.ListenDataChannels(peerConn, tun.Config.Logger)
			defer dcListener.Close()
			defer peerConn.Close()

			tun.Debug("handing peer off to socks5 server")
			if err := server.Serve(dcListener); err != nil {
				tun.Error("socks5 server exited with error", "err", err)
			}
			tun.Debug("socks5 serve for peer connection exited")
		}()
	}
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

func (tun *Tunnel) startClient(ctx context.Context, addr string) error {
	tun.Info("connecting to peer...")

	ncfg := tun.Config
	ncfg.Logger = tun.Config.Logger.With("where", "wrtc")
	pd, err := wrtc.NewPeerDialer(ctx, ncfg)
	if err != nil {
		return fmt.Errorf("failed to create peer dialer: %w", err)
	}

	pc, err := pd.Dial(ctx)
	if err != nil {
		return fmt.Errorf("failed to establish peer connection: %w", err)
	}

	tun.Info("peer connection established, listening for SOCKS5 connections", "addr", addr)

	// Log ICE/RTT stats for the lifetime of this peer connection.
	statsCtx, cancelStats := context.WithCancel(ctx)
	logPCStats(statsCtx, pc, tun.Config.Logger)
	defer cancelStats()

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer l.Close()

	connID := 0
	for {
		tun.Debug("waiting for incoming SOCKS5 TCP connection...")

		conn, err := l.Accept()
		if err != nil {
			return fmt.Errorf("TCP accept error: %w", err)
		}

		connID++
		id := connID
		tun.Debug("accepted SOCKS5 TCP connection", "conn_id", id, "remote", conn.RemoteAddr())

		go func(conn net.Conn, id int) {
			defer conn.Close()

			tun.Debug("creating datachannel for SOCKS5 connection", "conn_id", id)
			dc, err := wrtc.CreateDataChannel(ctx, pc, tun.Config.Logger)
			if err != nil {
				tun.Error("failed to create datachannel", "conn_id", id, "err", err)
				return
			}

			tun.Debug("datachannel ready, binding with TCP connection", "conn_id", id)
			if bindErr := bindConnections(conn, dc, id, tun.Config.Logger); bindErr != nil {
				tun.Debug("connection unbound", "conn_id", id, "err", bindErr)
			} else {
				tun.Debug("connection closed cleanly", "conn_id", id)
			}

			_ = dc.Close()
			tun.Debug("datachannel closed", "conn_id", id)
		}(conn, id)
	}
}

func (tun *Tunnel) StartClient(ctx context.Context, addr string) error {
	return tun.startClient(ctx, addr)
}

func (tun *Tunnel) StartServer(ctx context.Context) error {
	return tun.startServer(ctx)
}