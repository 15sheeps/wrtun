package max

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/15sheeps/wrtun/transport/max/calls"
	con "github.com/15sheeps/wrtun/transport/max/constants"
	"github.com/15sheeps/wrtun/transport/max/oneme"
	"github.com/mdp/qrterminal/v3"
	"github.com/pion/webrtc/v4"
)

type MaxClient struct {
	logger *slog.Logger
	*oneme.Client

	mu               sync.Mutex
	incomingMessages chan []byte
}

func NewClientWithContext(
	ctx context.Context,
	token string,
	logger *slog.Logger,
) (*MaxClient, error) {
	oc, err := oneme.NewClientWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize oneme client: %w", err)
	}

	mc := &MaxClient{
		logger:           logger,
		Client:           oc,
		incomingMessages: make(chan []byte, 100),
	}

	mc.Client.OnMessage(func(msg string) {
		logger.Debug("MAX: incoming chat message", "message", msg)

		msgBytes, err := base64.StdEncoding.DecodeString(msg)
		if err != nil || len(msgBytes) == 0 {
			return
		}
		mc.incomingMessages <- msgBytes
	})

	if _, err := mc.DoChatSync(ctx, token); err != nil {
		return nil, fmt.Errorf("chat sync failed: %w", err)
	}

	return mc, nil
}

func RetrieveToken(ctx context.Context) (string, error) {
	client, err := oneme.NewClientWithContext(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to initialize oneme client: %w", err)
	}

	pollData, err := client.DoQRAuthStart(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to start QR auth procedure: %w", err)
	}

	trackID := pollData.TrackID
	pollingInterval := time.Duration(pollData.PollingInterval) * time.Millisecond
	expiryTime := time.UnixMilli(pollData.ExpiresAt)

	qrterminal.Generate(pollData.QrLink, qrterminal.M, os.Stdout)

	// Create a context that expires when the QR code expires
	authCtx, cancel := context.WithDeadline(ctx, expiryTime)
	defer cancel()

	ticker := time.NewTicker(pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-authCtx.Done():
			if authCtx.Err() == context.DeadlineExceeded {
				return "", fmt.Errorf("QR code expired")
			}
			return "", fmt.Errorf("authentication cancelled: %w", authCtx.Err())
		case <-ticker.C:
			loggedIn, err := client.DoQRAuthPoll(ctx, trackID)
			if err != nil {
				return "", fmt.Errorf("failed to poll QR auth procedure: %w", err)
			}

			if !loggedIn {
				continue
			}

			authData, err := client.DoQRAuthFinish(ctx, trackID)
			if err != nil {
				return "", fmt.Errorf("failed to finish QR auth procedure: %w", err)
			}

			return authData.TokenAttrs.Login.Token, nil
		}
	}
}

func (client *MaxClient) GetICEServers(ctx context.Context) ([]webrtc.ICEServer, error) {
	callToken, err := client.GetCallToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get call token: %w", err)
	}

	joinLink, err := client.StartConversation(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start conversation: %w", err)
	}

	sessKey, err := calls.Login(ctx, callToken)
	if err != nil {
		return nil, fmt.Errorf("failed to login: %w", err)
	}

	iceInfo, err := calls.JoinConversation(ctx, joinLink, sessKey)
	if err != nil {
		return nil, fmt.Errorf("failed to join conversation: %w", err)
	}

	return []webrtc.ICEServer{
		iceInfo.TurnServer, iceInfo.StunServer,
	}, nil
}

func (client *MaxClient) Receive(ctx context.Context) ([]byte, error) {
	select {
	case msg, ok := <-client.incomingMessages:
		if !ok {
			return nil, fmt.Errorf("client has been closed")
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (client *MaxClient) Send(ctx context.Context, msg []byte) error {
	client.logger.Info("sending message", "msg", string(msg))

	encoded := base64.StdEncoding.EncodeToString(msg)

	msgLen := len(encoded)
	if msgLen > con.MAX_MESSAGE_LENGTH {
		return fmt.Errorf(
			"message to be send with length %d exceed maximum length of %d",
			msgLen, con.MAX_MESSAGE_LENGTH,
		)
	}

	if err := client.SendMessage(ctx, encoded, 0); err != nil {
		return err
	}

	return nil
}
