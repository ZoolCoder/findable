package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoolcoder/findable/internal/check"
	"github.com/zoolcoder/findable/internal/engine"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/findable/internal/sitetest"
)

// fixture writes the sitetest site and config and returns the config's
// path and the write_dir apply writes into.
func fixture(t *testing.T) (cfg, dir string) {
	t.Helper()
	root := t.TempDir()
	return sitetest.Write(t, root), filepath.Join(root, "public")
}

func run(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := runCLI(args, strings.NewReader(stdin), &out, &errOut)
	return out.String() + errOut.String(), err
}

func TestPlanApplyConverge(t *testing.T) {
	cfg, dir := fixture(t)
	out, err := run(t, "", "plan", "-config", cfg, "-exit-code")
	if !errors.Is(err, errDrift) || !strings.Contains(out, "CREATE  example  llms.txt") || !strings.Contains(out, "UPDATE  example  robots.txt") {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	if out, err = run(t, "", "apply", "-config", cfg); err != nil || !strings.Contains(out, "applied and verified") {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "llms.txt")); !strings.HasPrefix(string(b), "# Example\n") {
		t.Fatalf("llms.txt %q", b)
	}
	// What is left is the finding a rebuild clears: dist/ still lacks the block.
	if out, err := run(t, "", "plan", "-config", cfg, "-exit-code"); !errors.Is(err, errManualOnly) || !strings.Contains(out, "ai.crawler.allowed") {
		t.Fatalf("converged plan: %v\n%s", err, out)
	}
	if out, _ := run(t, "", "status", "-config", cfg); !strings.Contains(out, "2 pages, 0 errors, 1 warnings, 0 notes") {
		t.Fatalf("status %s", out)
	}
}

func TestSavedPlanRefusesWhenTheWorldMoved(t *testing.T) {
	cfg, dir := fixture(t)
	saved := filepath.Join(t.TempDir(), "plan.json")
	if _, err := run(t, "", "plan", "-config", cfg, "-json", "-out", saved); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(saved); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("saved plan %v %v", info, err)
	}
	_ = os.WriteFile(filepath.Join(dir, "llms.txt"), []byte("other"), 0o600)
	if _, err := run(t, "", "apply", "-config", cfg, saved); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("moved world applied: %v", err)
	}
	if _, err := run(t, "", "apply", "-config", cfg, "-target", "other", saved); err == nil {
		t.Fatal("different selection accepted")
	}
}

func TestAudit(t *testing.T) {
	cfg, _ := fixture(t)
	page := filepath.Join(filepath.Dir(cfg), "dist", "index.html")
	b, _ := os.ReadFile(page)
	_ = os.WriteFile(page, []byte(strings.Replace(string(b), "</main>", `<a href="/nope">x</a></main>`, 1)), 0o600)

	out, err := run(t, "", "audit", "-config", cfg)
	if err != nil || !strings.Contains(out, "error links.broken  links to /nope") || !strings.Contains(out, "fix: ") {
		t.Fatalf("audit: %v\n%s", err, out)
	}
	if _, err := run(t, "", "audit", "-config", cfg, "-exit-code"); !errors.Is(err, errFindings) {
		t.Fatalf("exit code: %v", err)
	}
	out, _ = run(t, "", "audit", "-config", cfg, "-json")
	var rows []struct {
		Target   string
		Findings []struct{ Check, Severity string }
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 1 || len(rows[0].Findings) != 2 {
		t.Fatalf("json: %v\n%s", err, out)
	}
}

func TestValidateDoctorVersionAndErrors(t *testing.T) {
	cfg, _ := fixture(t)
	if out, err := run(t, "", "validate", "-config", cfg); err != nil || !strings.Contains(out, "1 target(s), valid") {
		t.Fatalf("validate: %v %s", err, out)
	}
	out, err := run(t, "", "doctor", "-config", cfg, "-json")
	var report struct {
		Checks []struct{ Name, Status string }
	}
	if jerr := json.Unmarshal([]byte(out[:strings.LastIndex(out, "}")+1]), &report); jerr != nil || len(report.Checks) != 3 {
		t.Fatalf("doctor: %v %v\n%s", err, jerr, out)
	}
	if out, _ := run(t, "", "version"); out != "dev\n" {
		t.Fatalf("version %q", out)
	}
	if _, err := run(t, "", "nope"); err == nil {
		t.Fatal("unknown command accepted")
	}
	if out, err := run(t, "version: 1\ntargets: [{name: x, provider: site, url: https://example.com}]\n", "validate", "-config", "-"); err != nil {
		t.Fatalf("stdin config: %v %s", err, out)
	}
	out, _ = run(t, "", "help")
	for cmd := range optional {
		if !strings.Contains(out, name+" "+cmd) {
			t.Fatalf("usage lacks %s:\n%s", cmd, out)
		}
	}
}

func TestConfirm(t *testing.T) {
	for in, want := range map[string]bool{
		"yes\n": true, "YES\n": true, " yes\n": true, "yes": true,
		"y\n": false, "\n": false, "": false, "yes please\n": false,
	} {
		if got := confirm(strings.NewReader(in), &bytes.Buffer{}); got != want {
			t.Errorf("confirm(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestConfirmDeletes(t *testing.T) {
	var deletes plan.Plan
	deletes.Add(plan.Action{Op: plan.OpDelete, Target: "a", Resource: "old.txt"})
	var creates plan.Plan
	creates.Add(plan.Action{Op: plan.OpCreate, Target: "a", Resource: "new.txt"})

	for _, in := range []string{"no\n", "", "y\n"} {
		if err := confirmDeletes(deletes, false, false, strings.NewReader(in), &bytes.Buffer{}); !errors.Is(err, errCancelled) {
			t.Errorf("stdin %q: %v, want errCancelled", in, err)
		}
	}
	for _, in := range []string{"yes\n", "YES\n"} {
		if err := confirmDeletes(deletes, false, false, strings.NewReader(in), &bytes.Buffer{}); err != nil {
			t.Errorf("stdin %q: %v", in, err)
		}
	}
	if err := confirmDeletes(deletes, true, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Errorf("-yes: %v", err)
	}
	if err := confirmDeletes(deletes, false, true, strings.NewReader("yes\n"), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "pass -yes to confirm") {
		t.Errorf("stdin config: %v", err)
	}
	if err := confirmDeletes(creates, false, false, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Errorf("no deletes: %v", err)
	}
}

func TestAuditUnreadableTargetOutranksFindings(t *testing.T) {
	cfg, _ := fixture(t)
	page := filepath.Join(filepath.Dir(cfg), "dist", "index.html")
	b, _ := os.ReadFile(page)
	_ = os.WriteFile(page, []byte(strings.Replace(string(b), "</main>", `<a href="/nope">x</a></main>`, 1)), 0o600)
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("  - name: broken\n    provider: site\n    path: nodir\n    base_url: https://example.com\n")
	_ = f.Close()

	out, err := run(t, "", "audit", "-config", cfg, "-exit-code")
	if err == nil || errors.Is(err, errFindings) || !strings.Contains(err.Error(), "target broken") {
		t.Fatalf("want exit 1 for the broken target, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "== broken: error: ") || strings.Contains(out, "broken: 0 findings") {
		t.Fatalf("broken target header:\n%s", out)
	}
}

func TestHelpIsNotAnError(t *testing.T) {
	for _, cmd := range []string{"plan", "audit", "doctor", "ui"} {
		out, err := run(t, "", cmd, "-h")
		if !errors.Is(err, flag.ErrHelp) || !strings.Contains(out, "Exit codes:") {
			t.Errorf("%s -h: %v\n%s", cmd, err, out)
		}
	}
}

func TestFindConfigOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(envConfig, "")
	t.Chdir(t.TempDir())

	if got, want := findConfig(""), filepath.Join(home, ".config", "findable", "findable.yaml"); got != want {
		t.Errorf("nothing set: got %q, want %q", got, want)
	}

	if err := os.WriteFile("findable.yaml", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findConfig(""); got != "findable.yaml" {
		t.Errorf("local file: got %q", got)
	}

	t.Setenv(envConfig, "/e.yaml")
	if got := findConfig(""); got != "/e.yaml" {
		t.Errorf("env: got %q", got)
	}
	if got := findConfig("/f.yaml"); got != "/f.yaml" {
		t.Errorf("flag: got %q", got)
	}
}

func TestFlagsBeforeTheCommandAreRefused(t *testing.T) {
	cfg, dir := fixture(t)
	for _, args := range [][]string{{"-config", cfg, "apply"}, {"-yes", "-config", cfg, "apply"}} {
		_, err := run(t, "", args...)
		if err == nil || !strings.Contains(err.Error(), "put the command first") {
			t.Errorf("%v: want a command-first error, got %v", args, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "llms.txt")); err == nil {
		t.Error("a refused apply wrote files")
	}
}

func TestStatusUnreadableTargetFails(t *testing.T) {
	cfg, _ := fixture(t)
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("  - name: broken\n    provider: site\n    path: nodir\n    base_url: https://example.com\n")
	_ = f.Close()

	out, err := run(t, "", "status", "-config", cfg)
	if err == nil || !strings.Contains(err.Error(), "target broken") {
		t.Fatalf("want an error for the broken target, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "example") || !strings.Contains(out, "broken   error: ") {
		t.Errorf("every row is still printed:\n%s", out)
	}
}

func TestAuditOutputDropsControlCharacters(t *testing.T) {
	rows := []engine.TargetFindings{{Target: "t", Findings: []check.Finding{{
		Severity: check.Error, Check: "x.y", Message: "bad \x1b[2J\nerror forged  row", Fix: "fix\x07",
		Pages: []string{"/a\n== fake\x1b[2J"},
	}}}}
	var b bytes.Buffer
	if err := writeAudit(&b, rows); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(b.String(), '\x1b') || strings.ContainsRune(b.String(), '\x07') ||
		strings.Contains(b.String(), "\nerror forged") || strings.Contains(b.String(), "\n== fake") {
		t.Errorf("control characters reached the output:\n%q", b.String())
	}
}
