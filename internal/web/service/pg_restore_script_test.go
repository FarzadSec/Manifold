package service

import (
	"os"
	"path/filepath"
	"testing"
)

// pg_restore writes its script for its own version: a client of PostgreSQL 17 or
// newer sets transaction_timeout, which a PostgreSQL 16 server refuses.
func TestDropSettingsTheServerLacks(t *testing.T) {
	script := "SET statement_timeout = 0;\n" +
		"SET transaction_timeout = 0;\n" +
		"SET client_encoding = 'UTF8';\n" +
		"COPY public.audit_entries (id, detail) FROM stdin;\n" +
		"1\tSET transaction_timeout = 0;\n" +
		"SET transaction_timeout = 0;\n" +
		"\\.\n" +
		"SET default_table_access_method = heap;\n"
	known := map[string]bool{"statement_timeout": true, "client_encoding": true, "default_table_access_method": true}
	want := "SET statement_timeout = 0;\n" +
		"-- SET transaction_timeout = 0;\n" +
		"SET client_encoding = 'UTF8';\n" +
		"COPY public.audit_entries (id, detail) FROM stdin;\n" +
		"1\tSET transaction_timeout = 0;\n" +
		"SET transaction_timeout = 0;\n" +
		"\\.\n" +
		"SET default_table_access_method = heap;\n"
	if got := string(dropSettingsTheServerLacks([]byte(script), known)); got != want {
		t.Errorf("script =\n%s\nwant\n%s", got, want)
	}
}

// Debian's pg_wrapper picks the newest pg_dump but the local cluster's pg_restore,
// so a backup the panel made could not be read back; the panel takes the newest itself.
func TestPgClientToolTakesTheNewestVersion(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"9.6", "16", "18"} {
		bin := filepath.Join(root, dir, "bin")
		if err := os.MkdirAll(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "pg_restore"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	previous := pgClientDirs
	pgClientDirs = []string{filepath.Join(root, "*", "bin")}
	t.Cleanup(func() { pgClientDirs = previous })
	got, err := pgClientTool("pg_restore")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "18", "bin", "pg_restore"); got != want {
		t.Errorf("pgClientTool = %q, want %q", got, want)
	}
}
