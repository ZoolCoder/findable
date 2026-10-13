package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/zoolcoder/findable/internal/engine"
	"github.com/zoolcoder/findable/internal/ui"
	"github.com/zoolcoder/toolshell/activity"
	"github.com/zoolcoder/toolshell/guard"
)

func init() {
	optional["ui"] = runUI
	optionalUsage = append(optionalUsage,
		"  "+name+" ui       [-addr a]      the admin page, on loopback only\n")
}

func runUI(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "127.0.0.1:7780", "listen address; loopback only")
	path := flags.String("config", "", "config file, re-read on every request")
	data := flags.String("data", "", "directory for the activity log")
	if err := flags.Parse(args); err != nil {
		return err
	}
	dir := *data
	if dir == "" {
		dir = activity.DataDir(name)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveUI(ctx, *addr, findConfig(*path), dir, stdout)
}

func serveUI(ctx context.Context, addr, configPath, dataDir string, stdout io.Writer) error {
	page, err := ui.New(ui.Deps{
		Name:       name,
		ConfigPath: configPath,
		Activity:   &activity.Log{Path: filepath.Join(dataDir, "activity.jsonl")},
		Providers:  engine.Default(),
		Host:       addr,
	})
	if err != nil {
		return err
	}
	return guard.ListenAndServe(ctx, addr, page, func(host string) {
		fmt.Fprintf(stdout, "%s ui on http://%s/ (this machine only; Ctrl-C to stop)\n", name, host)
	})
}
