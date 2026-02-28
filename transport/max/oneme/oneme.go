package oneme

import (
	"io"
	"fmt"
	"sync"
	"time"
	"errors"
	"context"
	"log/slog"
	"net/http"
	"encoding/json"
	"github.com/coder/websocket"
	wsjson "github.com/coder/websocket/wsjson"
	"github.com/15sheeps/wrtun/transport/max/oneme/client"
	"github.com/15sheeps/wrtun/transport/max/oneme/server"
	con "github.com/15sheeps/wrtun/transport/max/constants"
)

var ErrClosedClient = errors.New("client has been closed.")

type CommandCallback func(msg Message[json.RawMessage])

type Client struct {
	mu        sync.Mutex
	seq       int
	done      chan struct{}
	conn      *websocket.Conn
	callbacks map[int]CommandCallback
	logger    *slog.Logger
}

const maxPayloadLogLen = 200

func truncatePayload(p []byte) string {
	s := string(p)
	if len(s) > maxPayloadLogLen {
		return s[:maxPayloadLogLen] + "...[truncated]"
	}
	return s
}

func NewClient(logger *slog.Logger) (*Client, error) {
	return NewClientWithContext(context.Background(), logger)
}

func NewClientWithContext(
	ctx context.Context, optLogger ...*slog.Logger,
) (*Client, error) {
	var logger *slog.Logger

	if len(optLogger) > 0 {
		logger = optLogger[0]
	} else {
		logger = slog.New(
			slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}),
		) 
	}

	header := http.Header{}
	header.Set("Origin", con.BASE_URL)
	opts := &websocket.DialOptions{HTTPHeader: header}

	conn, _, err := websocket.Dial(ctx, con.ONEME_ENDPOINT, opts)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	conn.SetReadLimit(-1)

	c := &Client{
		seq:       0,
		conn:      conn,
		done:      make(chan struct{}),
		callbacks: make(map[int]CommandCallback),
		logger:    logger,
	}

	go c.readLoop()
	go c.pingLoop(10 * time.Second)

	_, err = c.doClientHello(ctx)
	if err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("client hello: %w", err)
	}

	return c, nil
}

func (client *Client) Close() (err error) {
	close(client.done)

	if client.conn != nil {
		err = client.conn.Close(websocket.StatusNormalClosure, "")
	}
	return
}

func SendCommand[TReq any](
	ctx context.Context,
	c *Client,
	opcode int,
	payload TReq,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	messageSeq := c.seq
	msg := NewMessage(messageSeq, opcode, payload)

	if err := wsjson.Write(ctx, c.conn, msg); err != nil {
		return err
	}

	c.seq++
	return nil
}

func (client *Client) registerCallback(opcode int, callback CommandCallback) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.callbacks[opcode] = callback
}

func (client *Client) deregisterCallback(opcode int) {
	client.mu.Lock()
	defer client.mu.Unlock()
	delete(client.callbacks, opcode)
}

func (client *Client) pingLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	for {
		select {
		case <-client.done:
			return
		case <-ticker.C:
			client.logger.Debug("sending ping", "where", "oneme",)
			if err := client.Ping(context.Background()); err != nil {
				client.logger.Error("ping failed", "where", "oneme", "err", err)
			}
		}
	}
}

func (c *Client) readLoop() {
	for {
		var msg Message[json.RawMessage]

		if err := wsjson.Read(context.Background(), c.conn, &msg); err != nil {
			select {
			case <-c.done:
				c.logger.Debug("readLoop stopped (client closed)", "where", "oneme",)
				return
			default:
			}

			if websocket.CloseStatus(err) != -1 {
				c.logger.Error(
					"websocket closed unexpectedly",
					"where", "oneme",
					"close_status", websocket.CloseStatus(err),
					"err", err,
				)
			} else {
				c.logger.Error("oneme: readLoop read error", "err", err)
			}
			return
		}

		c.logger.Info("received message",
			"where", "oneme",
			"opcode", msg.Opcode,
			"seq", msg.Sequence,
			"payload", truncatePayload(msg.Payload),
		)

		c.mu.Lock()
		callback, exists := c.callbacks[msg.Opcode]
		c.mu.Unlock()

		if exists {
			c.logger.Debug(
				"dispatching message to callback", 
				"where", "oneme", 
				"opcode", msg.Opcode,
			)
			callback(msg)
		} else {
			c.logger.Debug(
				"no callback registered for opcode, dropping message", 
				"where", "oneme",
				"opcode", msg.Opcode,
			)
		}
	}
}

func sendAndReceive[TReq, TResp any](
	pctx context.Context,
	c *Client,
	opcode int,
	req TReq,
) (TResp, error) {
	ctx, cancel := context.WithCancelCause(pctx)
	defer cancel(nil)

	var result TResp
	done := make(chan struct{})
	var doneOnce sync.Once

	c.registerCallback(opcode, func(msg Message[json.RawMessage]) {
		if err := json.Unmarshal(msg.Payload, &result); err != nil {
			cancel(fmt.Errorf("json unmarshaling message with opcode %d: %w", opcode, err))
		}
		doneOnce.Do(func() { close(done) })
	})

	if err := SendCommand(ctx, c, opcode, req); err != nil {
		c.deregisterCallback(opcode)
		return result, err
	}

	c.logger.Debug("waiting for response", "where", "oneme", "opcode", opcode)

	select {
	case <-done:
		c.deregisterCallback(opcode)
		return result, context.Cause(ctx)
	case <-ctx.Done():
		c.deregisterCallback(opcode)
		return result, context.Cause(ctx)
	}
}

func (c *Client) doClientHello(ctx context.Context) (server.ClientHello, error) {
	return sendAndReceive[client.ClientHello, server.ClientHello](
		ctx, c, client.OP_CLIENT_HELLO, client.NewClientHello(),
	)
}

func (c *Client) DoChatSync(ctx context.Context, token string) (server.ChatSyncResponse, error) {
	return sendAndReceive[client.ChatSyncRequest, server.ChatSyncResponse](
		ctx, c, client.OP_CHAT_SYNC, client.NewChatSyncRequest(token),
	)
}

func (c *Client) DoQRAuthStart(ctx context.Context) (server.QRAuthStart, error) {
	return sendAndReceive[client.QRAuthStart, server.QRAuthStart](
		ctx, c, client.OP_QR_AUTH_START, client.QRAuthStart{},
	)
}

func (c *Client) DoQRAuthPoll(ctx context.Context, trackId string) (bool, error) {
	resp, err := sendAndReceive[client.QRAuthPoll, server.QRAuthPoll](
		ctx, c, client.OP_QR_AUTH_POLL, client.QRAuthPoll{TrackID: trackId},
	)
	if err != nil {
		return false, err
	}
	return resp.Status.LoginAvailable, nil
}

func (c *Client) DoQRAuthFinish(ctx context.Context, trackId string) (server.QRAuthFinish, error) {
	return sendAndReceive[client.QRAuthPoll, server.QRAuthFinish](
		ctx, c, client.OP_QR_AUTH_FINISH, client.QRAuthPoll{TrackID: trackId},
	)
}

func (c *Client) GetCallToken(ctx context.Context) (string, error) {
	resp, err := sendAndReceive[struct{}, server.CallToken](
		ctx, c, client.OP_CALL_TOKEN, struct{}{},
	)
	if err != nil {
		return "", err
	}
	return resp.Token, nil
}

func (c *Client) StartConversation(ctx context.Context) (string, error) {
	resp, err := sendAndReceive[client.StartConversation, server.StartConversation](
		ctx, c, client.OP_NEW_CONVO, client.NewStartConversation(),
	)
	if err != nil {
		return "", err
	}
	return resp.JoinLink, nil
}

func (c *Client) SendMessage(ctx context.Context, text string, chatId int) error {
	_, err := sendAndReceive[client.ChatMessage, struct{}](
		ctx, c, client.OP_SEND_MESSAGE, client.NewChatMessage(text, chatId),
	)
	return err
}

func (c *Client) Ping(ctx context.Context) error {
	return SendCommand[client.Ping](
		ctx, c, client.OP_PING, client.NewPing(),
	)
}

func (c *Client) OnMessage(f func(string)) {
	c.registerCallback(client.OP_RECV_MESSAGE, func(msg Message[json.RawMessage]) {
		var chatMsg server.ChatMessage

		if err := json.Unmarshal(msg.Payload, &chatMsg); err != nil {
			c.logger.Error("failed to unmarshal incoming chat message",
				"where", "oneme",
				"payload", string(msg.Payload),
				"err", err,
			)
			return
		}

		f(chatMsg.Message.Text)
	})
}
