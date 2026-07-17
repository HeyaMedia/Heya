package cmd

import (
	"fmt"

	"github.com/karbowiak/heya/internal/config"
	"github.com/karbowiak/heya/internal/database"
)

// validateActiveRuntimeDatabase prevents a source/dev process that owns
// migrations, workers, or filesystem mutations from accidentally targeting a
// production database. Published containers opt in explicitly.
func validateActiveRuntimeDatabase(c *config.Config, devBackend bool) error {
	if c.PassiveMode.Value {
		return nil
	}
	localDB, dbHost, err := database.AllHostsLocal(c.DatabaseURL.Value)
	if err != nil {
		return fmt.Errorf("refusing to start active mode: cannot parse HEYA_DATABASE_URL to verify the database host is local: %w", err)
	}
	if !localDB && (devBackend || !c.AllowRemoteActive.Value) {
		return fmt.Errorf("refusing to start active mode against non-local database host %q: "+
			"set HEYA_PASSIVE_MODE=true to use it read-only, point HEYA_DATABASE_URL at a local DB, "+
			"or set HEYA_ALLOW_REMOTE_ACTIVE=true if this instance is meant to own that DB "+
			"(--dev-backend can never run active against a remote DB)", dbHost)
	}
	return nil
}
