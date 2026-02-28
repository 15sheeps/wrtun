package client

type QRAuthStart struct{}

type QRAuthPoll struct {
	TrackID string `json:"trackId"`
}