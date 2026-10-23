package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/am-fallah/neda/internal/auth"
	"github.com/am-fallah/neda/internal/config"
	"github.com/am-fallah/neda/internal/db"
	"github.com/am-fallah/neda/internal/migrations"
	"github.com/am-fallah/neda/internal/store"
	"golang.org/x/term"
)

type passwordPrompter interface {
	IsTerminal() bool
	ReadPassword() (string, error)
}

type termPrompter struct {
	in *os.File
}

func (p termPrompter) IsTerminal() bool {
	return p.in != nil && term.IsTerminal(int(p.in.Fd()))
}

func (p termPrompter) ReadPassword() (string, error) {
	if p.in == nil {
		return "", fmt.Errorf("stdin is unavailable")
	}
	raw, err := term.ReadPassword(int(p.in.Fd()))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func runSetPassword(args []string, stdout, stderr io.Writer, prompter passwordPrompter) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "neda: set-password does not accept arguments")
		return 2
	}
	if prompter == nil || !prompter.IsTerminal() {
		fmt.Fprintln(stderr, "neda: set-password requires an interactive terminal")
		return 1
	}

	path, err := config.LoadDBPath()
	if err != nil {
		fmt.Fprintf(stderr, "neda: set-password configuration error: %v\n", err)
		return 1
	}
	ctx := context.Background()
	database, err := db.Open(ctx, path)
	if err != nil {
		fmt.Fprintf(stderr, "neda: set-password database error: %v\n", err)
		return 1
	}
	defer func() {
		if err := database.Close(); err != nil {
			fmt.Fprintf(stderr, "neda: set-password database close error: %v\n", err)
		}
	}()

	applied, err := db.Migrate(ctx, database.DB, migrations.FS)
	if err != nil {
		fmt.Fprintf(stderr, "neda: set-password migration error: %v\n", err)
		return 1
	}
	if err := database.Ready(ctx, applied); err != nil {
		fmt.Fprintf(stderr, "neda: set-password readiness error: %v\n", err)
		return 1
	}

	fmt.Fprint(stderr, "New password: ")
	password, err := prompter.ReadPassword()
	fmt.Fprintln(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "neda: read password: %v\n", err)
		return 1
	}
	fmt.Fprint(stderr, "Repeat password: ")
	repeat, err := prompter.ReadPassword()
	fmt.Fprintln(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "neda: read password: %v\n", err)
		return 1
	}
	if password != repeat {
		fmt.Fprintln(stderr, "neda: passwords do not match")
		return 1
	}

	if err := auth.SetPassword(ctx, store.New(database.DB), password, auth.DefaultParams, time.Now().UTC()); err != nil {
		fmt.Fprintf(stderr, "neda: set-password failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Password set for the Neda owner account.")
	return 0
}
