package imap

import (
	"context"
	"fmt"
)

// For the MVP we do not support IMAP-based signaling. This package is kept as
// a stub so that it compiles while the MAX-based transport is implemented.

type Client struct{}

type ClientConfig struct{}

func NewClient(_ *ClientConfig) (*Client, error) {
	return nil, fmt.Errorf("IMAP transport is not implemented yet")
}

func (c *Client) Receive(_ context.Context) ([]byte, error) {
	return nil, fmt.Errorf("IMAP transport is not implemented yet")
}

func (c *Client) Send(_ context.Context, _ []byte) error {
	return fmt.Errorf("IMAP transport is not implemented yet")
}
