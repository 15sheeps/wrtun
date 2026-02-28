package client

import "time"

func NewChatMessage(text string, chatId int) ChatMessage {
	return ChatMessage{
		ChatID: chatId,
		Notify: true,
		Message: Message{
			Text: text,
			Cid: -1 * time.Now().UnixMilli(),
			Elements: nil,
			Attaches: nil,
		},
	}
}

type Message struct {
	Text     string `json:"text"`
	Cid      int64  `json:"cid"`
	Elements []any  `json:"elements"`
	Attaches []any  `json:"attaches"`
}
type ChatMessage struct {
	ChatID  int     `json:"chatId"`
	Message Message `json:"message"`
	Notify  bool    `json:"notify"`
}