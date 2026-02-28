package constants

import "github.com/google/uuid"

var DEVICE_ID string

func init() {
	DEVICE_ID = uuid.NewString()
}

const (
	MAX_MESSAGE_LENGTH   = 4000
	ONEME_ENDPOINT 		 = "wss://ws-api.oneme.ru/websocket"
	ONEME_VERSION  		 = 11
	BASE_URL	   		 = "https://web.max.ru"
	USER_AGENT     		 = "Mozilla/5.0 (Windows NT 6.1; WOW64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.138 Safari/537.36 OPR/68.0.3618.173"
	CALLS_ENDPOINT 		 = "https://calls.okcdn.ru/fb.do"
	CALLS_VERSION  		 = 3
	CALLS_APP_KEY  		 = "CNHIJPLGDIHBABABA"
	CALLS_CLIENT_VERSION = 1.1
	CALLS_PROTO_VERSION  = "5"
)