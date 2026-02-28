package client

type Ping struct {
	Interactive bool `json:"interactive"`
}

func NewPing() Ping { return Ping{false} }