package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// pgClientDirs are where Debian and the PostgreSQL project's RPMs keep each major
// version's client programs, behind the version-picking wrappers in /usr/bin.
var pgClientDirs = []string{"/usr/lib/postgresql/*/bin", "/usr/pgsql-*/bin"}

// pgClientTool finds the newest installed version of a PostgreSQL client program.
// It reads every older server and archive, and pg_dump, pg_restore and psql agree.
func pgClientTool(name string) (string, error) {
	best, bestMajor := "", -1
	for _, pattern := range pgClientDirs {
		paths, _ := filepath.Glob(filepath.Join(pattern, name))
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				continue
			}
			versionDir := strings.TrimPrefix(filepath.Base(filepath.Dir(filepath.Dir(path))), "pgsql-")
			major, _, _ := strings.Cut(versionDir, ".")
			if n, err := strconv.Atoi(major); err == nil && n > bestMajor {
				best, bestMajor = path, n
			}
		}
	}
	if best != "" {
		return best, nil
	}
	return exec.LookPath(name)
}
