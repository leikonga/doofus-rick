package store

import "gorm.io/gorm"

func (s *Store) Db() *gorm.DB {
	return s.db
}

func (s *Store) GetQuotes() []Quote {
	var quotes []Quote
	s.db.Order("timestamp desc").Find(&quotes)
	return quotes
}
