package store

import (
	"context"
)

func (s *Store) GetQuotes(ctx context.Context) ([]Quote, error) {
	var quotes []Quote
	err := s.db.WithContext(ctx).Order("created_at desc").Find(&quotes).Error
	return quotes, err
}

func (s *Store) GetQuote(ctx context.Context, id string) (Quote, error) {
	var quote Quote
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&quote).Error
	return quote, mapNotFound(err)
}

func (s *Store) GetRandomQuote(ctx context.Context) (Quote, error) {
	var quote Quote
	err := s.db.WithContext(ctx).Order("random()").First(&quote).Error
	return quote, mapNotFound(err)
}

func (s *Store) CreateQuote(ctx context.Context, quote Quote) error {
	return s.db.WithContext(ctx).Create(&quote).Error
}

func (s *Store) GetQuotesByParticipant(ctx context.Context, userID string) ([]Quote, error) {
	var quotes []Quote
	err := s.db.WithContext(ctx).Where(`participants LIKE ?`, `%"`+userID+`"%`).Order("created_at desc").Find(&quotes).Error
	return quotes, err
}

func (s *Store) SearchQuotes(ctx context.Context, query string) ([]Quote, error) {
	var quotes []Quote
	q := s.db.WithContext(ctx).Order("created_at desc")
	if query != "" {
		q = q.Where("LOWER(content) LIKE LOWER(?)", "%"+query+"%")
	}
	err := q.Find(&quotes).Error
	return quotes, err
}

func (s *Store) GetQuotesByUser(ctx context.Context, userID string) ([]Quote, error) {
	var quotes []Quote
	err := s.db.WithContext(ctx).Where(`creator = ? OR participants LIKE ?`, userID, `%"`+userID+`"%`).Order("created_at desc").Find(&quotes).Error
	return quotes, err
}
