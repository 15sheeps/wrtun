package wrtc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v4"
)

var ErrPeerNotConnected = errors.New("peer is not connected")

const TunnelDataChannelName = "tdc"

const (
	// maxBufferedAmount is the high-water mark for a DataChannel's SCTP send
	// buffer. Writes are paused above this level to prevent a single large
	// transfer from saturating the shared SCTP association and starving other
	// concurrent DataChannels on the same PeerConnection.
	maxBufferedAmount uint64 = 1024 * 1024 // 256 KB

	// bufferedAmountLowThreshold is the low-water mark below which a paused
	// Write is allowed to resume after a backpressure event.
	bufferedAmountLowThreshold uint64 = 512 * 1024 // 64 KB
)

// DataChannel wraps a detached pion DataChannel and adds per-channel
// write-side flow control: writes are paused when the SCTP send buffer
// exceeds maxBufferedAmount and are resumed when it drains below
// bufferedAmountLowThreshold (via the OnBufferedAmountLow callback) or when
// the channel is closed.
type DataChannel struct {
	datachannel.ReadWriteCloserDeadliner

	// raw is kept after detaching solely for BufferedAmount() queries used
	// by the backpressure logic.
	raw *webrtc.DataChannel

	// sendMoreCh is signaled (non-blocking send) by OnBufferedAmountLow
	// whenever the SCTP buffer drains below bufferedAmountLowThreshold.
	// Capacity 1 acts as an edge-triggered latch: a signal that fires before
	// the Write goroutine reaches the select is not lost.
	sendMoreCh chan struct{}

	// closedCh is closed by our Close() override.  Write() selects on it so
	// that a goroutine blocked waiting for backpressure relief is always
	// unblocked when the channel is torn down.
	closedCh  chan struct{}
	closeOnce sync.Once
}

// Write implements per-channel write-side flow control.
//
// If the DataChannel's SCTP send buffer currently exceeds maxBufferedAmount
// the call blocks until one of two things happens:
//   - OnBufferedAmountLow fires, meaning the buffer drained below
//     bufferedAmountLowThreshold and it is safe to write again, or
//   - the DataChannel is closed (via Close()), in which case net.ErrClosed is
//     returned immediately so the caller's io.Copy loop can exit cleanly.
//
// This prevents a single large transfer from flooding the shared SCTP
// association and stalling every other DataChannel on the same PeerConnection.
func (dc *DataChannel) Write(p []byte) (int, error) {
	if dc.raw.BufferedAmount() > maxBufferedAmount {
		select {
		case <-dc.sendMoreCh:
			// Buffer drained; fall through to the actual write below.
		case <-dc.closedCh:
			return 0, net.ErrClosed
		}
	}
	return dc.ReadWriteCloserDeadliner.Write(p)
}

// Close closes the underlying DataChannel and unblocks any Write that is
// currently waiting for backpressure relief.  Safe to call multiple times.
func (dc *DataChannel) Close() error {
	dc.closeOnce.Do(func() { close(dc.closedCh) })
	return dc.ReadWriteCloserDeadliner.Close()
}

// These stubs allow a detached DataChannel to satisfy net.Conn.
func (dc *DataChannel) LocalAddr() net.Addr           { return nil }
func (dc *DataChannel) RemoteAddr() net.Addr          { return nil }
func (dc *DataChannel) SetDeadline(t time.Time) error { return nil }
func (dc *DataChannel) SetReadDeadline(t time.Time) error {
	return dc.ReadWriteCloserDeadliner.SetReadDeadline(t)
}
func (dc *DataChannel) SetWriteDeadline(t time.Time) error {
	return dc.ReadWriteCloserDeadliner.SetWriteDeadline(t)
}

// detachWithDeadline wires up the backpressure callbacks on rawDC before
// detaching it, so flow control is active from the very first byte.
func detachWithDeadline(rawDC *webrtc.DataChannel) (*DataChannel, error) {
	sendMoreCh := make(chan struct{}, 1)
	closedCh := make(chan struct{})

	// Tell pion at what buffer level to fire OnBufferedAmountLow.
	rawDC.SetBufferedAmountLowThreshold(bufferedAmountLowThreshold)

	// Non-blocking send: if the channel already holds a token the callback
	// is a no-op, which is correct — the Write goroutine will see the
	// existing token and re-check BufferedAmount() on its own.
	rawDC.OnBufferedAmountLow(func() {
		select {
		case sendMoreCh <- struct{}{}:
		default:
		}
	})

	detached, err := rawDC.DetachWithDeadline()
	if err != nil {
		return nil, err
	}

	return &DataChannel{
		ReadWriteCloserDeadliner: detached,
		raw:                      rawDC,
		sendMoreCh:               sendMoreCh,
		closedCh:                 closedCh,
	}, nil
}

// DataChannelListener accepts incoming data channels on an established
// PeerConnection (server side).
type DataChannelListener struct {
	closeOnce  sync.Once
	done       chan struct{}
	pc         *webrtc.PeerConnection
	discovered chan *DataChannel
	logger     *slog.Logger
}

// CreateDataChannel creates a new labelled data channel on pc and blocks until
// it is open and detached, or until ctx is cancelled.
func CreateDataChannel(
	ctx context.Context,
	pc *webrtc.PeerConnection,
	logger *slog.Logger,
) (*DataChannel, error) {
	if logger == nil {
		logger = slog.Default()
	}

	type result struct {
		dc  *DataChannel
		err error
	}
	resultCh := make(chan result, 1)

	dc, err := pc.CreateDataChannel(TunnelDataChannelName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}

	logger.Debug("data channel created, waiting for it to open...", "label", TunnelDataChannelName)

	dc.OnOpen(func() {
		logger.Debug("data channel opened, detaching...", "label", dc.Label())
		detached, detachErr := detachWithDeadline(dc)
		resultCh <- result{dc: detached, err: detachErr}
	})

	select {
	case res := <-resultCh:
		if res.err != nil {
			return nil, fmt.Errorf("failed to detach data channel: %w", res.err)
		}
		logger.Debug("data channel detached successfully", "label", TunnelDataChannelName)
		return res.dc, nil
	case <-ctx.Done():
		_ = dc.Close()
		return nil, fmt.Errorf("context cancelled while waiting for data channel to open: %w", ctx.Err())
	}
}

// ListenDataChannels returns a DataChannelListener that yields every new
// tunnel data channel opened on pc. The caller must ensure pc is already in
// the Connected state before calling this function (i.e. after handleOffer
// has waited for PeerConnectionStateConnected).
func ListenDataChannels(pc *webrtc.PeerConnection, logger *slog.Logger) *DataChannelListener {
	if logger == nil {
		logger = slog.Default()
	}

	listener := &DataChannelListener{
		done: make(chan struct{}),
		// Buffered so that OnDataChannel callbacks don't block when Accept is
		// temporarily not waiting.
		discovered: make(chan *DataChannel, 16),
		pc:         pc,
		logger:     logger,
	}

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Debug("server peer connection state changed inside listener", "state", state.String())
		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed,
			webrtc.PeerConnectionStateDisconnected:
			logger.Info("peer connection lost, closing DataChannelListener", "state", state.String())
			listener.Close()
		}
	})

	pc.OnDataChannel(func(rawDC *webrtc.DataChannel) {
		id := rawDC.ID()
		idVal := uint16(0)
		if id != nil {
			idVal = *id
		}
		logger.Debug("new incoming data channel",
			"id", idVal,
			"label", rawDC.Label(),
			"negotiated", rawDC.Negotiated(),
		)

		rawDC.OnOpen(func() {
			if rawDC.Label() != TunnelDataChannelName {
				logger.Debug("ignoring data channel with unexpected label",
					"label", rawDC.Label(),
					"expected", TunnelDataChannelName,
				)
				return
			}

			logger.Debug("tunnel data channel opened, detaching...", "id", idVal)
			detached, err := detachWithDeadline(rawDC)
			if err != nil {
				logger.Error("failed to detach incoming data channel", "err", err)
				return
			}

			logger.Debug("tunnel data channel detached, sending to Accept queue", "id", idVal)

			select {
			case listener.discovered <- detached:
			case <-listener.done:
				logger.Debug("listener closed while delivering data channel, discarding", "id", idVal)
				_ = detached.Close()
			}
		})
	})

	return listener
}

// Close stops the listener. Safe to call multiple times.
func (l *DataChannelListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.done)
	})
	return nil
}

// Addr is a stub so DataChannelListener can satisfy net.Listener.
func (l *DataChannelListener) Addr() net.Addr { return nil }

// Accept blocks until a new tunnel data channel is available or the listener
// is closed.
func (l *DataChannelListener) Accept() (net.Conn, error) {
	select {
	case dc := <-l.discovered:
		l.logger.Debug("Accept returning new data channel connection")
		return dc, nil
	case <-l.done:
		return nil, ErrPeerConnectionClosed
	}
}
