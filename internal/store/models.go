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

type BackfillState struct {
	ID            int    `gorm:"primaryKey;autoIncrement:false;default:1"`
	Status        string `gorm:"not null"`
	StartedAt     *time.Time
	FinishedAt    *time.Time
	LastError     *string
	ChannelsTotal int
	ChannelsDone  int
	MessagesSeen  int64     `gorm:"default:0"`
	UpdatedAt     time.Time `gorm:"not null"`
}

type BackfillChannel struct {
	ChannelID     uint64 `gorm:"primaryKey"`
	NewestAtStart *uint64
	OldestFetched *uint64
	Done          bool  `gorm:"default:false"`
	MessagesSeen  int64 `gorm:"default:0"`
	LastError     *string
	UpdatedAt     time.Time `gorm:"not null"`
}

type Chunk struct {
	ID             uint64    `gorm:"primaryKey"`
	ChannelID      uint64    `gorm:"not null;index:idx_chunks_channel_ended;index:idx_chunks_channel_last_msg,priority:1"`
	Content        string    `gorm:"not null"`
	StartedAt      time.Time `gorm:"not null"`
	EndedAt        time.Time `gorm:"not null"`
	MessageCount   int       `gorm:"not null"`
	FirstMessageID uint64    `gorm:"not null"`
	LastMessageID  uint64    `gorm:"not null;index:idx_chunks_channel_last_msg,priority:2"`
}

type ChunkEmbedding struct {
	ChunkID   uint64     `gorm:"primaryKey"`
	Model     string     `gorm:"primaryKey"`
	Embedding HalfVector `gorm:"type:halfvec(1024);not null"`
}

type UserAffinity struct {
	UserID     uint64 `gorm:"primaryKey"`
	Score      int    `gorm:"not null;default:-20"`
	LastReason *string
	UpdatedAt  time.Time `gorm:"not null"`
}

type AmbientLog struct {
	ID        uint64    `gorm:"primaryKey"`
	ChannelID uint64    `gorm:"not null"`
	FiredAt   time.Time `gorm:"not null"`
	Score     int       `gorm:"not null"`
	Hook      *string
}

type AmbientState struct {
	ChannelID             uint64 `gorm:"primaryKey"`
	LastEval              time.Time
	LastFire              *time.Time
	FiresToday            int
	LastUnpromptedID      *uint64
	LastUnpromptedIgnored bool
	UpdatedAt             time.Time `gorm:"not null"`
}
