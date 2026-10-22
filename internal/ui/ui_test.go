package ui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zoolcoder/findable/internal/engine"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/findable/internal/sitetest"
	"github.com/zoolcoder/toolshell/activity"
)

func newPage(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	cfg := sitetest.Write(t, root)
	s, err := New(Deps{Name: "tool", ConfigPath: cfg, Providers: engine.Default(),
		Activity: &activity.Log{Path: filepath.Join(root, "activity.jsonl")},
		Now:      func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return s, filepath.Join(root, "public")
}

func do(s *Server, method, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestPagesRender(t *testing.T) {
	s, _ := newPage(t)
	for path, want := range map[string]string{
		"/": "example", "/targets/example": "press", "/activity": "nothing recorded yet", "/settings": "site",
	} {
		w := do(s, http.MethodGet, path, nil)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: %d, lacks %q", path, w.Code, want)
		}
	}
	if w := do(s, http.MethodGet, "/targets/missing", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing target %d", w.Code)
	}
}

func TestPlanThenApplyBehindTypedName(t *testing.T) {
	s, dir := newPage(t)
	if w := do(s, http.MethodPost, "/targets/example/apply", url.Values{"confirm": {"example"}}); !strings.Contains(w.Header().Get("Location"), "err=plan+first") {
		t.Fatalf("apply before plan: %v", w.Header())
	}
	if w := do(s, http.MethodPost, "/targets/example/plan", nil); w.Code != http.StatusSeeOther {
		t.Fatalf("plan %d", w.Code)
	}
	if w := do(s, http.MethodGet, "/targets/example", nil); !strings.Contains(w.Body.String(), "CREATE") {
		t.Fatal("plan not shown")
	}
	if w := do(s, http.MethodPost, "/targets/example/apply", url.Values{"confirm": {"wrong"}}); !strings.Contains(w.Header().Get("Location"), "err=") {
		t.Fatal("wrong name accepted")
	}
	w := do(s, http.MethodPost, "/targets/example/apply", url.Values{"confirm": {"example"}})
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "msg=applied") {
		t.Fatalf("apply: %s", loc)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "llms.txt")); !strings.HasPrefix(string(b), "# Example") {
		t.Fatalf("llms.txt %q", b)
	}
	if w := do(s, http.MethodGet, "/activity", nil); !strings.Contains(w.Body.String(), "2 change(s) applied") {
		t.Fatal("apply not in activity")
	}
}

func TestApplyRefusesMovedState(t *testing.T) {
	s, dir := newPage(t)
	do(s, http.MethodPost, "/targets/example/plan", nil)
	_ = os.WriteFile(filepath.Join(dir, "llms.txt"), []byte("hand-written"), 0o600)
	if loc := do(s, http.MethodPost, "/targets/example/apply", url.Values{"confirm": {"example"}}).Header().Get("Location"); !strings.Contains(loc, "changed+since") {
		t.Fatalf("moved state applied: %s", loc)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "llms.txt")); string(b) != "hand-written" {
		t.Fatal("wrote after refusing")
	}
}

func TestClaimIsExclusivePerTarget(t *testing.T) {
	s, _ := newPage(t)
	if !s.claim("a") {
		t.Fatal("first claim refused")
	}
	if s.claim("a") {
		t.Fatal("second claim on a busy target granted")
	}
	if !s.claim("b") {
		t.Fatal("another target blocked")
	}
	s.release("a")
	if !s.claim("a") {
		t.Fatal("claim after release refused")
	}
}

func TestPageTextDropsControlCharacters(t *testing.T) {
	s, _ := newPage(t)
	var p plan.Plan
	p.Add(plan.Action{Op: plan.OpManual, Target: "example", Resource: "x", Detail: "bad \x1b[2J\x07forged"})
	s.setRun("example", run{Plan: p, Err: "failed \x1b[31m"})
	body := do(s, http.MethodGet, "/targets/example", nil).Body.String()
	if !strings.Contains(body, "bad [2Jforged") || strings.ContainsAny(body, "\x1b\x07") {
		t.Errorf("control characters reached the page:\n%q", body)
	}
}
