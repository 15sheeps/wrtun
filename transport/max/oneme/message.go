package oneme

import con "github.com/15sheeps/wrtun/transport/max/constants"

type MessagingSide int

const (
	MessagingSideClient MessagingSide = 0
	MessagingSideServer MessagingSide = 1
)

type Message[T any] struct {
	Sequence 	   int           `json:"seq"`
	Opcode         int           `json:"opcode"`
	Version        int           `json:"ver"`
	Payload        T             `json:"payload"`
	Side           MessagingSide `json:"cmd"`
}

func NewMessage[T any](sequence, opcode int, payload T) Message[T] {
	return Message[T]{
		Sequence: 		sequence,
		Opcode:         opcode,
		Version:        con.ONEME_VERSION,
		Payload:        payload,
		Side:           MessagingSideClient,
	}
}
