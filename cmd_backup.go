package main

import (
	"context"
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

const backupUsage = `Usage:
  manifold backup decrypt -in FILE -out FILE [-passphrase P]
  manifold backup restore -in FILE [-passphrase P] [-keep-access=false] [-rewrite-address] [-force]

The passphrase may also come from MANIFOLD_BACKUP_PASSPHRASE.
restore moves a full backup onto this server, as the panel's restore does;
stop the panel first (systemctl stop manifold), then start it again.
`

// runBackupCLI opens sealed backups and restores a full backup without the panel running.
func runBackupCLI(args []string) int {
	if len(args) == 0 {
		fmt.Print(backupUsage)
		return 2
	}
	fs := flag.NewFlagSet("backup "+args[0], flag.ContinueOnError)
	in := fs.String("in", "", "backup file to read")
	out := fs.String("out", "", "file to write (decrypt)")
	passphrase := fs.String("passphrase", os.Getenv("MANIFOLD_BACKUP_PASSPHRASE"), "passphrase of a sealed backup")
	keepAccess := fs.Bool("keep-access", true, "keep this server's panel port, path and login")
	rewrite := fs.Bool("rewrite-address", false, "replace the old server's address with this one's")
	force := fs.Bool("force", false, "restore even though the panel service is running")
	if err := fs.Parse(args[1:]); err != nil || *in == "" {
		fmt.Print(backupUsage)
		return 2
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		fmt.Println("read:", err)
		return 1
	}
	archive, err := service.OpenBackup(data, *passphrase)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	switch args[0] {
	case "decrypt":
		if *out == "" {
			fmt.Print(backupUsage)
			return 2
		}
		if err := os.WriteFile(*out, archive, 0o600); err != nil {
			fmt.Println("write:", err)
			return 1
		}
		fmt.Println("Wrote", *out)
		return 0
	case "restore":
		if !*force && exec.CommandContext(context.Background(), "systemctl", "is-active", "--quiet", "manifold").Run() == nil {
			fmt.Println("the panel is running: stop it first (systemctl stop manifold), or pass -force")
			return 1
		}
		if err := database.InitDB(config.GetDBPath()); err != nil {
			fmt.Println("open database:", err)
			return 1
		}
		report, err := (&service.ServerService{}).RestoreBackup(bytes.NewReader(archive), service.RestoreOptions{
			KeepPanelAccess: *keepAccess, RewriteAddress: *rewrite,
		})
		if report != nil {
			for _, issue := range report.Issues {
				fmt.Printf("- %s %s %v\n", issue.Level, issue.Code, issue.Params)
			}
		}
		if err != nil {
			fmt.Println("restore failed:", err)
			return 1
		}
		fmt.Println("Restored. Start the panel: systemctl start manifold")
		return 0
	}
	fmt.Print(backupUsage)
	return 2
}
