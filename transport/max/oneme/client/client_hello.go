package client

import con "github.com/15sheeps/wrtun/transport/max/constants"

type DeviceType string

const (
	DeviceTypeWEB DeviceType = "WEB"
)

type UserAgent struct {
	DeviceType      DeviceType `json:"deviceType"`
	Locale          string     `json:"locale"`
	DeviceLocale    string     `json:"deviceLocale"`
	OSVersion       string     `json:"osVersion"`
	DeviceName      string     `json:"deviceName"`
	HeaderUserAgent string     `json:"headerUserAgent"`
	AppVersion      string     `json:"appVersion"`
	Screen          string     `json:"screen"`
	Timezone        string     `json:"timezone"`
}

func NewUserAgent() UserAgent {
	return UserAgent{
		DeviceType:      DeviceTypeWEB,
		Locale:          "ru",
		DeviceLocale:    "ru",
		OSVersion:       "Windows",
		DeviceName:      "Chrome",
		HeaderUserAgent: con.USER_AGENT,
		AppVersion:      "25.12.14",
		Screen:          "1080x1920 1.0x",
		Timezone:        "Europe/Moscow",
	}
}

type ClientHello struct {
	UserAgent UserAgent `json:"userAgent"`
	DeviceID  string    `json:"deviceId"`
}

func NewClientHello() ClientHello {
	return ClientHello{
		UserAgent: NewUserAgent(),
		DeviceID:  con.DEVICE_ID,
	}
}