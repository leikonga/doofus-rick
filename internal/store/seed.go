package store

import (
	"context"
	"log/slog"
)

// MaybeSeed inserts example quotes if the quotes table is empty.
func (s *Store) MaybeSeed(ctx context.Context) {
	var count int64
	s.db.WithContext(ctx).Model(&Quote{}).Count(&count)
	if count > 0 {
		return
	}

	quotes := []Quote{
		{
			Content:      "I am the master of my own destiny, and my destiny is snacks.",
			Creator:      "000000000000000001",
			Participants: StringSlice{"000000000000000001", "000000000000000002"},
		},
		{
			Content:      "We did not set the server on fire. The fire set itself on fire.",
			Creator:      "000000000000000002",
			Participants: StringSlice{"000000000000000002", "000000000000000003"},
		},
		{
			Content:      "Technically correct is the best kind of correct, and I am never technically correct.",
			Creator:      "000000000000000003",
			Participants: StringSlice{"000000000000000001", "000000000000000003"},
		},
		{
			Content:      "The deployment went fine. The users are just wrong.",
			Creator:      "000000000000000001",
			Participants: StringSlice{"000000000000000001"},
		},
		{
			Content:      "I read the docs. Well, I read the title of the docs.",
			Creator:      "000000000000000002",
			Participants: StringSlice{"000000000000000002", "000000000000000003"},
		},
	}

	for _, q := range quotes {
		if err := s.db.WithContext(ctx).Create(&q).Error; err != nil {
			slog.Warn("failed to seed quote", "error", err)
		}
	}

	slog.Info("seeded example quotes", "count", len(quotes))
}
