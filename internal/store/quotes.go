package store

import "context"

func (s *Store) GetQuotes(ctx context.Context) []Quote {
	var quotes []Quote
	s.db.WithContext(ctx).Order("created_at desc").Find(&quotes)
	return quotes
}

func (s *Store) GetQuote(ctx context.Context, id string) (Quote, error) {
	var quote Quote
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&quote).Error
	return quote, err
}

func (s *Store) GetRandomQuote(ctx context.Context) (Quote, error) {
	var quote Quote
	err := s.db.WithContext(ctx).Order("random()").First(&quote).Error
	return quote, err
}

func (s *Store) CreateQuote(ctx context.Context, quote Quote) error {
	return s.db.WithContext(ctx).Create(&quote).Error
}

func (s *Store) GetQuotesByParticipant(ctx context.Context, userID string) []Quote {
	var quotes []Quote
	s.db.WithContext(ctx).Where("? = ANY(participants)", userID).Order("created_at desc").Find(&quotes)
	return quotes
}

func (s *Store) SearchQuotes(ctx context.Context, query string) []Quote {
	var quotes []Quote
	s.db.WithContext(ctx).Where("content ILIKE ?", "%"+query+"%").Order("created_at desc").Find(&quotes)
	return quotes
}
