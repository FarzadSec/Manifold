package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
)

// deviceIndexName is the unique index of client_hwids. A backup can carry the same
// device twice under it, so it is built only after the rows are merged.
const deviceIndexName = "idx_client_hwids_sub_hash"

// mergeDuplicateDevicesSQL folds the rows of one device of one subscription into
// one: the earliest first sighting and, for the rest, the newest row.
const mergeDuplicateDevicesSQL = `
DO $merge$
DECLARE merged bigint;
BEGIN
	IF to_regclass('public.client_hwids') IS NULL THEN
		RETURN;
	END IF;
	UPDATE public.client_hwids k SET first_seen = d.first_seen
	FROM (SELECT sub_id, hwid_hash, min(first_seen) AS first_seen FROM public.client_hwids
		GROUP BY sub_id, hwid_hash HAVING count(*) > 1) d
	WHERE k.sub_id = d.sub_id AND k.hwid_hash = d.hwid_hash;
	DELETE FROM public.client_hwids a USING public.client_hwids b
	WHERE a.sub_id = b.sub_id AND a.hwid_hash = b.hwid_hash AND (a.last_seen, a.id) < (b.last_seen, b.id);
	GET DIAGNOSTICS merged = ROW_COUNT;
	IF merged > 0 THEN
		RAISE NOTICE 'merged % duplicate device rows of the backup', merged;
	END IF;
END
$merge$;
`

var settingLine = regexp.MustCompile(`^SET ([a-z_]+) = .*;$`)

// serverSettings lists the parameters the server has, for dropSettingsTheServerLacks.
func serverSettings(psql string, env []string, dbname string) (map[string]bool, error) {
	cmd := exec.CommandContext(context.Background(), psql, "--no-psqlrc", "--tuples-only", "--no-align",
		"--dbname", dbname, "--command", "SELECT name FROM pg_catalog.pg_settings")
	cmd.Env = env
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, common.NewErrorf("Cannot read the PostgreSQL server's settings: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	known := make(map[string]bool)
	for name := range strings.FieldsSeq(out.String()) {
		known[name] = true
	}
	return known, nil
}

// dropSettingsTheServerLacks comments out the script's SET lines for parameters the
// server lacks, as pg_restore does when it talks to a server itself; COPY data stays.
func dropSettingsTheServerLacks(script []byte, known map[string]bool) []byte {
	var out bytes.Buffer
	inCopy := false
	for line := range bytes.Lines(script) {
		text := string(bytes.TrimSuffix(line, []byte("\n")))
		switch {
		case inCopy:
			inCopy = text != `\.`
		case strings.HasPrefix(text, "COPY ") && strings.HasSuffix(text, " FROM stdin;"):
			inCopy = true
		default:
			if m := settingLine.FindStringSubmatch(text); m != nil && !known[m[1]] {
				out.WriteString("-- ")
			}
		}
		out.Write(line)
	}
	return out.Bytes()
}

// splitRestoreList separates the device index entry from the rest of a
// "pg_restore --list" table of contents.
func splitRestoreList(toc string) (rest, deferred string) {
	var restLines, deferredLines []string
	for line := range strings.SplitSeq(toc, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 6 && !strings.HasPrefix(line, ";") &&
			fields[3] == "INDEX" && fields[4] == "public" && fields[5] == deviceIndexName {
			deferredLines = append(deferredLines, line)
			continue
		}
		restLines = append(restLines, line)
	}
	return strings.Join(restLines, "\n"), strings.Join(deferredLines, "\n")
}

func runPgRestore(bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), bin, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, common.NewErrorf("pg_restore %s failed: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

// writeRestoreScript turns a custom-format dump into one SQL script for psql: the
// dump without the device index, the device merge, then the device index.
func writeRestoreScript(pgRestore, dumpPath, dir string, known map[string]bool) (string, error) {
	toc, err := runPgRestore(pgRestore, "--list", dumpPath)
	if err != nil {
		return "", err
	}
	rest, deferred := splitRestoreList(string(toc))
	restList, deferredList := filepath.Join(dir, "rest.list"), filepath.Join(dir, "deferred.list")
	if err := os.WriteFile(restList, []byte(rest), 0o600); err != nil {
		return "", err
	}
	restSQL, err := runPgRestore(pgRestore, "--use-list="+restList, "--clean", "--if-exists",
		"--no-owner", "--no-privileges", "--file=-", dumpPath)
	if err != nil {
		return "", err
	}
	var script bytes.Buffer
	script.Write(dropSettingsTheServerLacks(restSQL, known))
	script.WriteString(mergeDuplicateDevicesSQL)
	if deferred != "" {
		if err := os.WriteFile(deferredList, []byte(deferred), 0o600); err != nil {
			return "", err
		}
		indexSQL, err := runPgRestore(pgRestore, "--use-list="+deferredList, "--no-owner", "--no-privileges", "--file=-", dumpPath)
		if err != nil {
			return "", err
		}
		script.Write(dropSettingsTheServerLacks(indexSQL, known))
	}
	scriptPath := filepath.Join(dir, "restore.sql")
	if err := os.WriteFile(scriptPath, script.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("write restore script: %w", err)
	}
	return scriptPath, nil
}
