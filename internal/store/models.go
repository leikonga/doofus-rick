package store

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type Quote struct {
	gorm.Model

	Content      string         `gorm:"not null"`
	Creator      string         `gorm:"not null"`
	Participants pq.StringArray `gorm:"type:text[]"`
	Votes        int            `gorm:"not null;default:0"`
}

type Memory struct {
	gorm.Model

	UserID  string         `gorm:"index"`
	Content string         `gorm:"not null"`
	Tags    pq.StringArray `gorm:"type:text[]"`
}

type Reminder struct {
	gorm.Model

	ChannelID string    `gorm:"not null"`
	UserID    string    `gorm:"not null"`
	Message   string    `gorm:"not null"`
	FireAt    time.Time `gorm:"type:timestamptz;not null;index"`
	Fired     bool      `gorm:"not null;default:false"`
}
