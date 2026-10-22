// Command findable audits a website for search and AI search: audit lists
// every finding with its fix, plan adds the files it can write (the robots.txt
// AI crawler block and llms.txt), apply writes them and verifies.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/engine"
	"github.com/zoolcoder/findable/internal/plan"
)

const (
	name      = "findable"
	envConfig = "FINDABLE_CONFIG"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// command is one subcommand: the same signature as runCLI, so a command is
// testable without a process.
type command func(args []string, stdin io.Reader, stdout, stderr io.Writer) error

// optional holds the commands that live in their own files (ui.go, mcp.go)
// and register themselves, so a project can delete one without editing this
// file. optionalUsage is their lines of the usage text.
var (
	optional      = map[string]command{}
	optionalUsage []string
)

const usageHead = name + ` audits a website for search and AI search, from findable.yaml.

Usage:
  ` + name + ` plan     [flags]        files to write, and every finding as MANUAL (default)
  ` + name + ` apply    [flags] [file] write the files; with a saved plan, exactly those
  ` + name + ` status   [flags]        pages and findings per target
  ` + name + ` validate [flags]        check the config offline
  ` + name + ` doctor   [-json]        install health: binary, config, targets
`

const usageTail = `  ` + name + ` version

Exit codes:
  0  nothing to do, or the apply converged and was verified
  1  something failed
  2  with -exit-code: changes are outstanding; audit: an error finding
  3  with -exit-code: nothing left but MANUAL items

Flags:
  -config string   config file, or "-" for standard input; default: $` + envConfig + `,
                   then ./` + name + `.yaml, then ~/.config/` + name + `/` + name + `.yaml
  -target value    limit to this target; repeat for several
  -yes             skip the confirmation prompt for deletions
  -exit-code       plan: exit 2 for changes, 3 for manual-only
  -json            audit, plan, status and doctor: machine-readable
  -out string      plan: save the plan, to apply exactly what you reviewed
`

func usage() string {
	return usageHead + strings.Join(optionalUsage, "") + usageTail
}

// -exit-code's signals, not failures: the plan printed fine.
var (
	errDrift      = errors.New("the live state differs from the config")
	errManualOnly = errors.New("only manual items are outstanding")
	errCancelled  = errors.New("cancelled")
)

func main() {
	err := runCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	case errors.Is(err, errDrift), errors.Is(err, errFindings):
		os.Exit(2)
	case errors.Is(err, errManualOnly):
		os.Exit(3)
	default:
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type targetList []string

func (t *targetList) String() string { return strings.Join(*t, ",") }
func (t *targetList) Set(v string) error {
	*t = append(*t, v)
	return nil
}

func isCommand(word string) bool {
	switch word {
	case "version", "help", "doctor", "plan", "apply", "status", "validate":
		return true
	}
	_, ok := optional[word]
	return ok
}

// runStatus prints every row, then fails when a target could not be read, as
// audit does.
func runStatus(ctx context.Context, eng *engine.Engine, asJSON bool, stdout io.Writer) error {
	rows := eng.Status(ctx)
	if err := writeStatus(stdout, rows, asJSON); err != nil {
		return err
	}
	for _, r := range rows {
		if r.Error != "" {
			return fmt.Errorf("target %s: %s", r.Target, plan.Printable(r.Error))
		}
	}
	return nil
}

// runCLI is main without the process, so tests drive the CLI as a shell would.
func runCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := "plan"
	defaulted := len(args) == 0 || strings.HasPrefix(args[0], "-")
	if !defaulted {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "version":
		_, err := fmt.Fprintln(stdout, version)
		return err
	case "help":
		_, err := fmt.Fprint(stdout, usage())
		return err
	case "doctor":
		return runDoctor(args, stdin, stdout, stderr)
	case "plan", "apply", "status", "validate":
	default:
		if run, ok := optional[cmd]; ok {
			return run(args, stdin, stdout, stderr)
		}
		fmt.Fprint(stderr, usage())
		return fmt.Errorf("unknown command %q", cmd)
	}

	flags := flag.NewFlagSet(cmd, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, usage()) }
	path := flags.String("config", "", "config file")
	yes := flags.Bool("yes", false, "skip the deletion prompt")
	exitCode := flags.Bool("exit-code", false, "exit 2 for changes, 3 for manual-only")
	asJSON := flags.Bool("json", false, "machine-readable output")
	out := flags.String("out", "", "save the plan to this file")
	var targets targetList
	flags.Var(&targets, "target", "limit to this target")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if defaulted && isCommand(flags.Arg(0)) {
		return fmt.Errorf("put the command first: %s %s [flags], not flags before %s", name, flags.Arg(0), flags.Arg(0))
	}
	*path = findConfig(*path)

	cfg, err := config.Load(*path, stdin)
	if err != nil {
		return err
	}
	if cmd == "validate" {
		_, err := fmt.Fprintf(stdout, "%s: %d target(s), valid\n", *path, len(cfg.Targets))
		return err
	}

	// A saved plan is re-planned for the targets it was made for, so a bare
	// `apply file` checks the plan that was reviewed and not a bigger one.
	var saved *plan.Saved
	if file := flags.Arg(0); cmd == "apply" && file != "" {
		s, err := readSavedPlan(file)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			targets = s.Targets
		} else if !slices.Equal(targets, s.Targets) {
			return fmt.Errorf("this plan was saved for -target %v; apply it with the same selection, or none", s.Targets)
		}
		saved = &s
	}

	eng, err := engine.New(cfg, engine.Default(), targets)
	if err != nil {
		return err
	}
	ctx := context.Background()

	if cmd == "status" {
		return runStatus(ctx, eng, *asJSON, stdout)
	}

	p, err := eng.Plan(ctx)
	if err != nil {
		return err
	}
	if cmd == "plan" {
		return runPlan(p, targets, *out, *asJSON, *exitCode, stdout, stderr)
	}

	if saved != nil {
		if err := saved.Matches(p); err != nil {
			return err
		}
	}
	if err := p.Write(stdout); err != nil {
		return err
	}
	if len(p.Executable()) == 0 {
		return nil
	}
	if err := confirmDeletes(p, *yes, *path == "-", stdin, stdout); err != nil {
		return err
	}
	fmt.Fprintln(stdout)
	return eng.Apply(ctx, p, stdout)
}

func runPlan(p plan.Plan, targets []string, out string, asJSON, exitCode bool, stdout, stderr io.Writer) error {
	write := p.Write
	if asJSON {
		write = p.WriteJSON
	}
	if err := write(stdout); err != nil {
		return err
	}
	if out != "" {
		if err := savePlan(p, targets, out); err != nil {
			return err
		}
		// With -json, stdout is the document; the notice must not break it.
		notice := stdout
		if asJSON {
			notice = stderr
		}
		fmt.Fprintf(notice, "\nsaved to %s; apply it with: %s apply %s\n", out, name, out)
	}
	if exitCode {
		switch {
		case len(p.Executable()) > 0:
			return errDrift
		case len(p.Manual()) > 0:
			return errManualOnly
		}
	}
	return nil
}

func writeStatus(w io.Writer, rows []engine.TargetStatus, asJSON bool) error {
	if asJSON {
		return writeJSON(w, rows)
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r.Target))
	}
	for _, r := range rows {
		line := plan.Printable(r.Summary)
		if r.Error != "" {
			line = "error: " + plan.Printable(r.Error)
		}
		if _, err := fmt.Fprintf(w, "%-*s  %s\n", width, plan.Printable(r.Target), line); err != nil {
			return err
		}
	}
	return nil
}

// confirmDeletes guards apply: a plan that deletes needs -yes or a typed yes,
// and a stdin config leaves no stdin to type on.
func confirmDeletes(p plan.Plan, yes, configFromStdin bool, stdin io.Reader, stdout io.Writer) error {
	if !p.Destructive() || yes {
		return nil
	}
	if configFromStdin {
		return errors.New("this plan deletes things and the config came from stdin; pass -yes to confirm")
	}
	if !confirm(stdin, stdout) {
		return errCancelled
	}
	return nil
}

func confirm(stdin io.Reader, stdout io.Writer) bool {
	fmt.Fprint(stdout, "\nthis plan deletes things. type yes to continue: ")
	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(answer), "yes")
}

// findConfig picks the config: the flag, the environment, the working
// directory, then the user's config directory.
func findConfig(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv(envConfig); env != "" {
		return env
	}
	local := name + ".yaml"
	if _, err := os.Stat(local); err == nil {
		return local
	}
	if dir, err := os.UserConfigDir(); err == nil {
		if home, err := os.UserHomeDir(); err == nil {
			// ~/.config on every platform: a CLI's config belongs where its
			// users look, not in ~/Library on macOS.
			dir = filepath.Join(home, ".config")
		}
		return filepath.Join(dir, name, name+".yaml")
	}
	return local
}

func savePlan(p plan.Plan, targets []string, path string) error {
	// 0600: a plan names paths and resources, which is nobody else's
	// business on a shared machine.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	defer func() { _ = f.Close() }()
	return p.Save(f, targets)
}

func readSavedPlan(path string) (plan.Saved, error) {
	f, err := os.Open(path)
	if err != nil {
		return plan.Saved{}, fmt.Errorf("read saved plan: %w", err)
	}
	defer func() { _ = f.Close() }()
	return plan.LoadSaved(f)
}
