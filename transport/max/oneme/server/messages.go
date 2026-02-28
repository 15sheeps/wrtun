package server

type StartConversation struct {
	JoinLink string `json:"joinLink"`
}

type CallToken struct {
	Token string `json:"token"`
}

type ClientHello struct {
	PhoneAuthEnabled bool     `json:"phone-auth-enabled"`
	RegCountryCode   []string `json:"reg-country-code"`
	Location         string   `json:"location"`
}

type VerificationToken struct {
	Token string `json:"token"`
}

type ChatSyncResponse struct {
	Token *string `json:"token,omitempty"`
}

type (
	QRAuthStart struct {
		PollingInterval int    `json:"pollingInterval"`
		QrLink          string `json:"qrLink"`
		TrackID         string `json:"trackId"`
		ExpiresAt       int64  `json:"expiresAt"`
	}

	QRAuthPoll struct {
		Status Status `json:"status"`
		Error  string `json:"error,omitempty"`
	}

	QRAuthFinish struct {
		TokenAttrs TokenAttrs `json:"tokenAttrs"`
	}

	Status struct {
		LoginAvailable bool  `json:"loginAvailable,omitempty"`
		ExpiresAt      int64 `json:"expiresAt"`
	}
	TokenAttrs struct {
		Login Login `json:"LOGIN"`
	}
	Login struct {
		Token string `json:"token"`
	}
)

type (
	Message struct {
		Text string `json:"text"`
	}
	ChatMessage struct {
		ChatID        int64   `json:"chatId"`
		Unread        int     `json:"unread"`
		Invisible     bool    `json:"invisible"`
		Message       Message `json:"message"`
		TTL           bool    `json:"ttl"`
		Mark          int64   `json:"mark"`
		PrevMessageID string  `json:"prevMessageId"`
	}
)
