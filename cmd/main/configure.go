package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/viper"

	"github.com/15sheeps/wrtun/pkg/wrtc"
	"github.com/15sheeps/wrtun/providers/public"
	maxtransport "github.com/15sheeps/wrtun/transport/max"
)

type (
	Transport string
	Provider  string
)

const (
	MAXTransport    Transport = "max"
	IMAPTransport   Transport = "imap"
	ManualTransport Transport = "manual"

	MAXProvider    Provider = "max"
	PublicProvider Provider = "public"
	VKProvider     Provider = "vk"
	YandexProvider Provider = "yandex"
)

// configure constructs a signaling transport and ICE provider based on
// command-line selections.
func configure(
	ctx context.Context,
	tr Transport,
	pr Provider,
) (wrtc.Transport, wrtc.Provider, error) {
	switch tr {
	case MAXTransport:
		mc, err := setupMAXClient(ctx)
		if err != nil {
			return nil, nil, err
		}

		switch pr {
		case MAXProvider:
			// MaxClient implements both wrtc.Transport and wrtc.Provider,
			// so it serves as both the signaling channel and the ICE server
			// source (TURN credentials come from the MAX calls API).
			return mc, mc, nil

		case PublicProvider:
			// Use MAX only for signaling; fetch ICE servers from the
			// public always-online-stun list instead of the MAX TURN relay.
			// This is useful for testing raw P2P throughput without a TURN
			// relay in the path: if both peers can reach each other directly
			// the connection will be established via STUN srflx candidates
			// and all data flows peer-to-peer.
			pr := public.NewWebSTUNProvider(public.DefaultURL)
			return mc, pr, nil

		default:
			return nil, nil, fmt.Errorf("unsupported provider %q for transport %q", pr, tr)
		}

	default:
		return nil, nil, fmt.Errorf(
			"unsupported transport/provider combination: %s/%s",
			tr, pr,
		)
	}
}

// setupMAXClient authenticates with the MAX service and returns an initialised
// client.  It reads MAX_TOKEN from the .env file; if the token is absent it
// starts the QR-code login flow and persists the resulting token.
func setupMAXClient(ctx context.Context) (*maxtransport.MaxClient, error) {
	var fileLookupError viper.ConfigFileNotFoundError
	if err := v.ReadInConfig(); err != nil {
		switch {
		case errors.As(err, &fileLookupError):
			logger.Warn("no .env file found")
		default:
			logger.Warn("error reading .env file", "err", err)
		}
	}

	token := v.GetString("MAX_TOKEN")
	if token == "" {
		logger.Warn("MAX_TOKEN missing from .env, starting QR auth flow")

		var err error
		token, err = maxtransport.RetrieveToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("MAX auth via QR failed: %w", err)
		}

		v.Set("MAX_TOKEN", token)
		if err := v.WriteConfig(); err != nil {
			logger.Warn("error writing token to .env file", "err", err)
		}
	}

	mc, err := maxtransport.NewClientWithContext(ctx, token, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create MAX client: %w", err)
	}

	return mc, nil
}
