package calls

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-querystring/query"

	con "github.com/15sheeps/wrtun/transport/max/constants"
	"io"
)

func ExecuteMethod(ctx context.Context, msg *Message, result any) error {
	values, err := query.Values(msg)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	params := strings.NewReader(values.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, con.CALLS_ENDPOINT, params)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", con.BASE_URL)
	req.Header.Set("Referer", con.BASE_URL)
	req.Header.Set("User-Agent", con.USER_AGENT)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if err = json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("unmarshal response: %w", err)
	}

	return nil
}

func Login(ctx context.Context, token string) (string, error) {
	sessionData := &SessionData{
		Version:       con.CALLS_VERSION,
		DeviceID:      con.DEVICE_ID,
		ClientVersion: con.CALLS_CLIENT_VERSION,
		ClientType:    "SDK_JS",
		AuthToken:     token,
	}

	sessionDataJson, err := json.Marshal(sessionData)
	if err != nil {
		return "", fmt.Errorf("marshal session data: %w", err)
	}

	var resp AnonymLoginResponse
	if err := ExecuteMethod(ctx, &Message{
		ApplicationKey: con.CALLS_APP_KEY,
		Format:         "JSON",
		Method:         MethodAnonymLogin,
		SessionData:    string(sessionDataJson),
	}, &resp); err != nil {
		return "", fmt.Errorf("failed to execute %s method: %w", MethodAnonymLogin, err)
	}

	return resp.SessionKey, nil
}

func JoinConversation(ctx context.Context, joinLink, sessKey string) (*JoinConversationResponse, error) {
	joinConvo := &Message{
		ApplicationKey:  con.CALLS_APP_KEY,
		Format:          "JSON",
		Method:          MethodJoinConversation,
		SessionKey:      sessKey,
		ProtocolVersion: con.CALLS_PROTO_VERSION,
		IsVideo:         "false",
		JoinLink:        joinLink,
	}

	var resp JoinConversationResponse
	if err := ExecuteMethod(ctx, joinConvo, &resp); err != nil {
		return nil, fmt.Errorf("failed to execute %s method: %w", MethodJoinConversation, err)
	}

	return &resp, nil
}
