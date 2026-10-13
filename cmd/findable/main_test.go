package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
