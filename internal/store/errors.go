package store

import (
	"errors"

	"gorm.io/gorm"
)

var ErrNotFound = errors.New("not found")

func mapNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
