package config

import (
	"sync"
)

const MaxPacketSize = 4000

var PacketsPool = sync.Pool{
	New: func() any {
		return make([]byte, MaxPacketSize)
	},
}