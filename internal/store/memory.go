package store

import "context"

func (s *Store) SaveMemory(ctx context.Context, userID, content string, tags []string) error {
	return s.db.WithContext(ctx).Create(&Memory{
		UserID:  userID,
		Content: content,
		Tags:    (*StringSlice)(&tags),
	}).Error
}

func (s *Store) SearchMemory(ctx context.Context, query, userID string) ([]Memory, error) {
	var memories []Memory
	db := s.db.WithContext(ctx).Order("created_at desc").Limit(10)
	if userID != "" {
		db = db.Where("user_id = ? OR user_id = ''", userID)
	}
	if query != "" {
		db = db.Where("LOWER(content) LIKE LOWER(?)", "%"+query+"%")
	}
	return memories, db.Find(&memories).Error
}
