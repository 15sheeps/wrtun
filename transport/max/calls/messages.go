package calls

import "github.com/pion/webrtc/v4"

type Method string
const (
	MethodAnonymLogin 	   Method	= "auth.anonymLogin"
	MethodJoinConversation Method   = "vchat.joinConversationByLink"
)

type SessionData struct {
	Version       int     `json:"version"`
	DeviceID      string  `json:"device_id"`
	ClientVersion float64 `json:"client_version"`
	ClientType    string  `json:"client_type"`
	AuthToken     string  `json:"auth_token"`
}

type Message struct {
	ApplicationKey  string `url:"application_key"`
	Format          string `url:"format"`
	IsVideo         string `url:"isVideo,omitempty"`
	JoinLink        string `url:"joinLink,omitempty"`
	Method          Method `url:"method"`
	Payload         string `url:"payload,omitempty"`
	ProtocolVersion string `url:"protocolVersion,omitempty"`
	SessionKey      string `url:"session_key,omitempty"`
	SessionData     string `url:"session_data,omitempty"`
}

type AnonymLoginResponse struct {
	UID              string `json:"uid"`
	SessionKey       string `json:"session_key"`
	SessionSecretKey string `json:"session_secret_key"`
	APIServer        string `json:"api_server"`
	ExternalUserID   string `json:"external_user_id"`
}

type JoinConversationResponse struct {
	Token        string     	  `json:"token"`
	Endpoint     string     	  `json:"endpoint"`
	WtEndpoint   string     	  `json:"wt_endpoint"`
	TurnServer   webrtc.ICEServer `json:"turn_server"`
	StunServer   webrtc.ICEServer `json:"stun_server"`
	ClientType   string     	  `json:"client_type"`
	DeviceIdx    int        	  `json:"device_idx"`
	ID           string     	  `json:"id"`
	P2PForbidden bool       	  `json:"p2p_forbidden"`
}