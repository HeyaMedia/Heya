package service

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgTimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// pgText wraps a string into pgtype.Text, treating "" as SQL NULL. Use for
// nullable text columns where empty-string and absent should be the same.
func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}
