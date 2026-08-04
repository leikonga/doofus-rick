package store

import (
	"time"

	"gorm.io/gorm"
)

type Quote struct {
	gorm.Model

	Content      string       `gorm:"not null"`
	Creator      string       `gorm:"not null"`
	Participants *StringSlice `gorm:"type:text"`
	Votes        int          `gorm:"not null;default:0"`
}

type Reminder struct {
	gorm.Model

	ChannelID string    `gorm:"not null"`
	UserID    string    `gorm:"not null"`
	Message   string    `gorm:"not null"`
	FireAt    time.Time `gorm:"type:timestamptz;not null;index"`
	Fired     bool      `gorm:"not null;default:false"`
}

type TokenUsage struct {
	gorm.Model

	ChannelID    string `gorm:"not null;index"`
	UserID       string `gorm:"not null;index"`
	ModelName    string `gorm:"not null"`
	InputTokens  int64  `gorm:"not null"`
	OutputTokens int64  `gorm:"not null"`
}

type FailureTrace struct {
	gorm.Model

	TraceID   string `gorm:"not null;uniqueIndex"`
	ChannelID string `gorm:"not null;index"`
	UserID    string `gorm:"not null"`
	Blob      string `gorm:"type:text;not null"`
	Decline   bool   `gorm:"not null"`
	ErrMsg    string `gorm:"type:text"`
}

type Message struct {
	ID          uint64 `gorm:"primaryKey"`
	ChannelID   uint64 `gorm:"not null;index:idx_messages_channel_created"`
	AuthorID    uint64 `gorm:"not null;index:idx_messages_author_created"`
	AuthorName  string `gorm:"not null"`
	Content     string `gorm:"not null"`
	ReplyToID   *uint64
	IsBot       bool
	Attachments *string
	CreatedAt   time.Time `gorm:"not null"`
	EditedAt    *time.Time
}

type ForgottenAuthor struct {
	UserID    uint64    `gorm:"primaryKey"`
	CreatedAt time.Time `gorm:"not null"`
}
