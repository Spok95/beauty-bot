package adminchat

import "time"

type Message struct {
	ID                   int64
	SenderUserID         int64
	SenderTelegramID     int64
	SenderUsername       string
	SenderRole           string
	MessageType          string
	Text                 string
	Caption              string
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MimeType             string
	FileSize             int64
	TelegramMessageID    int64
	TelegramMediaGroupID string
	CreatedAt            time.Time
}

type CreateMessageInput struct {
	SenderUserID         int64
	SenderTelegramID     int64
	SenderUsername       string
	SenderRole           string
	MessageType          string
	Text                 string
	Caption              string
	TelegramFileID       string
	TelegramFileUniqueID string
	FileName             string
	MimeType             string
	FileSize             int64
	TelegramMessageID    int64
	TelegramMediaGroupID string
}
