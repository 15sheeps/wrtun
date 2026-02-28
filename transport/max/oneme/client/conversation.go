package client

import "github.com/google/uuid"

type StartConversation struct {
	ConversationID string `json:"conversationId"`
}

func NewStartConversation() StartConversation {
	return StartConversation{
		ConversationID: uuid.NewString(),
	}
}