package store

func (s *Store) GetQuotes() []Quote {
	var quotes []Quote
	s.db.Order("timestamp desc").Find(&quotes)
	return quotes
}
