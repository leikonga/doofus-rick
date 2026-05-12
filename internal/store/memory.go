package store

func (s *Store) SaveMemory(userID, content string, tags []string) error {
	return s.db.Create(&Memory{
		UserID:  userID,
		Content: content,
		Tags:    tags,
	}).Error
}

func (s *Store) SearchMemory(query, userID string) ([]Memory, error) {
	var memories []Memory
	db := s.db.Order("created_at desc").Limit(10)
	if userID != "" {
		db = db.Where("user_id = ? OR user_id = ''", userID)
	}
	if query != "" {
		db = db.Where("content ILIKE ?", "%"+query+"%")
	}
	return memories, db.Find(&memories).Error
}
