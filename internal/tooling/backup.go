package tooling

import (
	"context"
	"fmt"
	"io"

	"github.com/ravinsharma7/missis/internal/application"
	"github.com/ravinsharma7/missis/pkg/missis"
)

func RunBackup(args []string, stdout, stderr io.Writer) int {
	return runBackup(args, stdout, stderr, "store-backup", false)
}

func RunBackupWithName(args []string, stdout, stderr io.Writer, commandName string) int {
	return runBackup(args, stdout, stderr, commandName, true)
}

func runBackup(args []string, stdout, stderr io.Writer, commandName string, exactArgs bool) int {
	_, stderr = commandWriters(stdout, stderr)
	if (exactArgs && len(args) != 1) || (!exactArgs && len(args) < 1) {
		fmt.Fprintf(stderr, "usage: %s <destination>\n", commandName)
		if exactArgs {
			return 2
		}
		return 1
	}
	svc, err := application.Open("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	client := missis.NewClient(svc)
	defer client.Close()
	if err := client.BackupTo(context.Background(), args[0]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
