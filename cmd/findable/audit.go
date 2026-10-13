package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/engine"
)

func init() {
	optional["audit"] = runAudit
	optionalUsage = append(optionalUsage,
		"  "+name+" audit    [flags]        every finding, worst first, with all its pages\n")
}

var errFindings = errors.New("the audit found errors")

// runAudit prints the findings plan lists as MANUAL lines, grouped by
// severity and with every page named. With -exit-code it fails on errors,
// so CI can gate a deploy on it.
func runAudit(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("audit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "config file")
	asJSON := flags.Bool("json", false, "machine-readable output")
	exitCode := flags.Bool("exit-code", false, "exit 2 when any finding is an error")
	var targets targetList
	flags.Var(&targets, "target", "limit to this target")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(findConfig(*path), stdin)
	if err != nil {
		return err
	}
	eng, err := engine.New(cfg, engine.Default(), targets)
	if err != nil {
		return err
	}
	rows := eng.Audit(context.Background())
	if *asJSON {
		err = writeJSON(stdout, rows)
	} else {
		err = writeAudit(stdout, rows)
	}
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Error != "" {
			return fmt.Errorf("target %s: %s", r.Target, r.Error)
		}
		for _, f := range r.Findings {
			if *exitCode && f.Severity == "error" {
				return errFindings
			}
		}
	}
	return nil
}

func writeAudit(w io.Writer, rows []engine.TargetFindings) error {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "== %s: %d findings\n", r.Target, len(r.Findings))
		if r.Error != "" {
			fmt.Fprintf(&b, "error: %s\n", r.Error)
		}
		for _, f := range r.Findings {
			fmt.Fprintf(&b, "\n%-5s %s  %s\n", f.Severity, f.Check, f.Message)
			fmt.Fprintf(&b, "      fix: %s\n", f.Fix)
			const shown = 8
			for i, p := range f.Pages {
				if i == shown {
					fmt.Fprintf(&b, "      ... and %d more (-json lists them all)\n", len(f.Pages)-shown)
					break
				}
				fmt.Fprintf(&b, "      %s\n", p)
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
