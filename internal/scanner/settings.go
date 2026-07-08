package scanner

import (
	"github.com/karbowiak/heya/internal/database/sqlc"
	"github.com/karbowiak/heya/internal/metadata"
)

func libraryUsesLocalData(lib sqlc.Library) bool {
	return metadata.ParseSettings(lib.Settings).UseLocalData
}
