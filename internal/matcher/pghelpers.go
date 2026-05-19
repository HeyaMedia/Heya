package matcher

import (
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func pgInt4FromString(s string) pgtype.Int4 {
	if s == "" {
		return pgtype.Int4{}
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(n), Valid: true}
}

func pgInt8(n int64) pgtype.Int8 {
	return pgtype.Int8{Int64: n, Valid: true}
}

func numericFromFloat(f float64) pgtype.Numeric {
	if f == 0 {
		return pgtype.Numeric{Valid: true}
	}
	intVal := int64(f * 1000)
	return pgtype.Numeric{
		Int:   big.NewInt(intVal),
		Exp:   -3,
		Valid: true,
	}
}

func pgDateFromString(s string) pgtype.Date {
	if s == "" {
		return pgtype.Date{}
	}

	formats := []string{"2006-01-02", "2006-01", "2006"}
	for _, fmt := range formats {
		if t, err := time.Parse(fmt, s); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}

	if len(s) >= 4 {
		s = strings.TrimSpace(s[:4])
		if t, err := time.Parse("2006", s); err == nil {
			return pgtype.Date{Time: t, Valid: true}
		}
	}

	return pgtype.Date{}
}
