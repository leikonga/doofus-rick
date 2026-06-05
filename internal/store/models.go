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

type Memory struct {
	gorm.Model

	UserID  string       `gorm:"index"`
	Content string       `gorm:"not null"`
	Tags    *StringSlice `gorm:"type:text"`
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
