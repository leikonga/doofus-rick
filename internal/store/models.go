package store

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

type Quote struct {
	gorm.Model

	ID           uint           `gorm:"primaryKey"`
	Content      string         `gorm:"not null"`
	Creator      string         `gorm:"not null"`
	Timestamp    time.Time      `gorm:"type:timestamp(3);default:CURRENT_TIMESTAMP"`
	Participants pq.StringArray `gorm:"type:text[]"`
	Votes        int            `gorm:"not null;default:0"`
}
