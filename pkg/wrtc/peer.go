// Package wrtc provides primitives to establish a client-server style WebRTC
// connection over an arbitrary duplex signaling transport.
package wrtc

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"time"
	"fmt"
	"log/slog"
	"sync"
//	"github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/protocol/handshake"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

var ErrPeerConnectionClosed = errors.New("peer conection have been closed")

type (
	// Transport defines the interface for the underlying communication channel
	// used for WebRTC signaling (HTTP long polling, WebSocket, IM, etc).
	Transport interface {
		Send(ctx context.Context, msg []byte) error
		Receive(ctx context.Context) ([]byte, error)
	}

	// Provider defines an interface for obtaining ICE servers.
	Provider interface {
		GetICEServers(ctx context.Context) ([]webrtc.ICEServer, error)
	}

	signalingMessage struct {
		ID uuid.UUID `json:"sid"`
		webrtc.SessionDescription
	}

	// PeerListener listens for incoming peer connections over the signaling
	// transport (server side).
	PeerListener struct {
		config    Config
		peerChan  chan *webrtc.PeerConnection
		errChan   chan error
		closeOnce sync.Once
		done      chan struct{}
	}

	PeerDialer struct {
		config Config
	}

	Config struct {
		Transport
		Provider
		*slog.Logger
		iceServers []webrtc.ICEServer
	}
)

func (cfg *Config) sendDescription(
	ctx context.Context,
	desc *webrtc.SessionDescription,
	id uuid.UUID,
) error {
	sig := &signalingMessage{ID: id, SessionDescription: *desc}

	jsonSig, err := json.Marshal(sig)
	if err != nil {
		return fmt.Errorf("failed to marshal signaling message: %w", err)
	}

	return cfg.Send(ctx, jsonSig)
}

func (cfg *Config) receiveDescription(
	ctx context.Context,
	wid ...uuid.UUID,
) (desc *webrtc.SessionDescription, gid uuid.UUID, err error) {
	for {
		select {
		case <-ctx.Done():
			return nil, gid, ctx.Err()
		default:
		}

		msg, err := cfg.Receive(ctx)
		if err != nil {
			return nil, gid, err
		}

		var sig signalingMessage
		if err := json.Unmarshal(msg, &sig); err != nil {
			cfg.Info(
				"failed to unmarshal signaling message",
				"message", string(msg),
				"error", err,
			)
			continue
		}

		gid = sig.ID
		desc = &sig.SessionDescription

		if len(wid) == 0 || wid[0] == gid {
			break
		}
	}

	return
}

func NewPeerDialer(ctx context.Context, cfg Config) (pd *PeerDialer, err error) {
	cfg.iceServers, err = cfg.Provider.GetICEServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ICE servers: %w", err)
	}

	pd = &PeerDialer{
		config: cfg,
	}

	return
}

func ListenPeerConnections(ctx context.Context, cfg Config) (pl *PeerListener, err error) {
	cfg.iceServers, err = cfg.Provider.GetICEServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ICE servers: %w", err)
	}

	pl = &PeerListener{
		config:   cfg,
		done:     make(chan struct{}),
		errChan:  make(chan error),
		peerChan: make(chan *webrtc.PeerConnection),
	}

	go pl.serve()

	return
}

func (pd *PeerDialer) Dial(ctx context.Context) (*webrtc.PeerConnection, error) {
	pc, err := pd.config.createPeerConn()
	if err != nil {
		return nil, fmt.Errorf("failed to create new peer connection: %w", err)
	}

	if err = pd.config.negotiateOfferer(ctx, pc); err != nil {
		pc.Close()
		return nil, err
	}

	return pc, nil
}

func (pl *PeerListener) serve() {
	for {
		select {
		case <-pl.done:
			return
		default:
		}

		pl.config.Debug("waiting for incoming offer on signaling transport...")

		offer, id, err := pl.config.receiveDescription(context.Background())
		if err != nil {
			pl.Close()
			pl.config.Error("error receiving offer from transport", "err", err)
			return
		}

		pl.config.Debug(
			"received offer, spawning handler goroutine",
			"id", id,
		)

		go func(
			offer *webrtc.SessionDescription, id uuid.UUID,
		) {
			pc, err := pl.config.negotiateAnswerer(context.Background(), offer, id)
			if err != nil {
				if pc != nil {
					pc.Close()
				}
				pl.config.Error(
					"error handling offer",
					"id", id,
					"err", err,
				)
				pl.errChan <- err
				return
			}
			pl.config.Info(
				"peer connection established",
				"id", id,
			)
			pl.peerChan <- pc
		}(offer, id)
	}
}

func (pl *PeerListener) Accept() (*webrtc.PeerConnection, error) {
	select {
	case err := <-pl.errChan:
		return nil, err
	case peer := <-pl.peerChan:
		return peer, nil
	case <-pl.done:
		return nil, fmt.Errorf("PeerListener is closed")
	}
}

func (pl *PeerListener) Close() error {
	pl.closeOnce.Do(func() {
		close(pl.done)
	})

	return nil
}

func (cfg *Config) negotiateAnswerer(
	pctx context.Context,
	offer *webrtc.SessionDescription,
	id uuid.UUID,
) (*webrtc.PeerConnection, error) {
	ctx, cancel := context.WithCancelCause(pctx)
	defer cancel(nil)

	pc, err := cfg.createPeerConn()
	if err != nil {
		return nil, fmt.Errorf("failed to create new peer connection: %w", err)
	}

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		cfg.Debug("client peer connection state changed", "state", state.String())
		switch state {
		case webrtc.PeerConnectionStateConnected:
			cancel(nil)
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			pc.Close()
			cancel(ErrPeerConnectionClosed)
		}
	})

	if err := pc.SetRemoteDescription(*offer); err != nil {
		return nil, fmt.Errorf("setting remote description: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, fmt.Errorf("creating answer: %w", err)
	}

	if err = pc.SetLocalDescription(answer); err != nil {
		return nil, fmt.Errorf("setting local description: %w", err)
	}

	cfg.Debug("gathering ICE candidates...")
	<-webrtc.GatheringCompletePromise(pc)
	cfg.Debug("ICE gathering complete, sending offer")

	if err = cfg.sendDescription(ctx, pc.LocalDescription(), id); err != nil {
		return nil, fmt.Errorf("sending offer: %w", err)
	}

	cfg.Debug("answer sent, waiting for peer connection to become connected...")

	select {
	case <-pctx.Done():
		return nil, pctx.Err()
	case <-ctx.Done():
		if err = ctx.Err(); err != context.Canceled {
			return nil, err
		}
	}

	return pc, nil
}

func (cfg *Config) negotiateOfferer(
	pctx context.Context,
	pc *webrtc.PeerConnection,
) error {
	id := uuid.New()
	ctx, cancel := context.WithCancelCause(pctx)
	defer cancel(nil)

	// webrtc obliges us to add at least one datachannel/transceiver to
	// the peer connection before generating an offer
	_, err := pc.CreateDataChannel("init", nil)
	if err != nil {
		return fmt.Errorf("failed to create the datachannel: %w", err)
	}

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		cfg.Debug("client peer connection state changed", "state", state.String())
		switch state {
		case webrtc.PeerConnectionStateConnected:
			cancel(nil)
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			cancel(ErrPeerConnectionClosed)
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("error creating offer: %w", err)
	}

	if err = pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("setting local description: %w", err)
	}

	cfg.Debug("gathering ICE candidates...")
	<-webrtc.GatheringCompletePromise(pc)
	cfg.Debug("ICE gathering complete, sending offer")

	if err = cfg.sendDescription(ctx, pc.LocalDescription(), id); err != nil {
		return fmt.Errorf("sending offer: %w", err)
	}

	cfg.Debug("offer sent, waiting for answer...")

	answer, _, err := cfg.receiveDescription(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to receive answer: %w", err)
	}

	cfg.Debug("answer received, setting remote description")

	if err = pc.SetRemoteDescription(*answer); err != nil {
		return fmt.Errorf("setting remote description: %w", err)
	}

	cfg.Debug("remote description set, waiting for peer connection to become connected...")

	select {
	case <-pctx.Done():
		return pctx.Err()
	case <-ctx.Done():
		if err = ctx.Err(); err != context.Canceled {
			return err
		}
	}

	return nil
}

func (cfg *Config) createPeerConn() (*webrtc.PeerConnection, error) {
	engine := webrtc.SettingEngine{}

	engine.DetachDataChannels()
	rand.Seed(time.Now().UnixNano())
	// The default pion/sctp receive buffer is 64 KB (myRecvBufSize = 65536).
	// SCTP flow control limits the sender to at most a_rwnd bytes in-flight,
	// so throughput is capped at:
	//
	//   throughput ≈ recv_window / RTT
	//
	// With the 64 KB default and a ~580 ms path RTT that gives exactly the
	// observed ~110 KB/s ceiling (64*1024 / 0.58 ≈ 110 KB/s).
	// Raising it to 16 MB lets the SCTP sender keep up to 16 MB in-flight,
	// which at 580 ms RTT raises the ceiling to ~27 MB/s — well above what
	// the underlying link can deliver.
	engine.SetSCTPMaxReceiveBufferSize(32 * 1024 * 1024)
	engine.SetEphemeralUDPPortRange(56985, 58135)
	engine.SetDTLSInsecureSkipHelloVerify(true)

	engine.SetDTLSCipherSuites()
	engine.DisableCertificateFingerprintVerification(true)
	api := webrtc.NewAPI(webrtc.WithSettingEngine(engine))

	conn, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         cfg.iceServers,
		ICETransportPolicy: webrtc.ICETransportPolicyAll,
	})
	if err != nil {
		return nil, err
	}

	return conn, nil
}
