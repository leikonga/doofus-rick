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

func (s *Store) GetQuote(id string) (Quote, error) {
	var quote Quote
	err := s.db.Where("id = ?", id).First(&quote).Error
	return quote, err
}

func (s *Store) GetRandomQuote() Quote {
	var quote Quote
	s.db.Order("random()").First(&quote)
	return quote
}

func (s *Store) CreateQuote(quote Quote) error {
	return s.db.Create(&quote).Error
}
