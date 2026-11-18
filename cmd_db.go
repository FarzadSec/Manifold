package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
)

// copyPostgresToSQLite writes the panel's PostgreSQL database into a new SQLite
// file, the one a switch back to SQLite moves into place.
func copyPostgresToSQLite(dst string) error {
	if config.GetDBKind() != "postgres" || config.GetDBDSN() == "" {
		return errors.New("the panel does not run on PostgreSQL: MANIFOLD_DB_TYPE=postgres and MANIFOLD_DB_DSN are not set")
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := database.ExportPostgresToSQLite(config.GetDBDSN(), dst); err != nil {
		_ = os.Remove(dst)
		return err
	}
	if err := database.ValidateSQLiteDB(dst); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("the copy did not open as a SQLite database: %w", err)
	}
	return nil
}
