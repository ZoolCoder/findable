package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/findable/internal/sitetest"
)

// stuck accepts every write and changes nothing, so verify must catch it.
type stuck struct{}

func (stuck) Plan(context.Context, config.Target) ([]plan.Action, error) {
	return []plan.Action{{Op: plan.OpCreate, Target: "t", Resource: "r",
		Do: func(context.Context) error { return nil }}}, nil
}
func (stuck) Status(context.Context, config.Target) (string, error) { return "stuck", nil }

// cfg points two targets at the fixture site; b asks apply for llms.txt.
func cfg(t *testing.T, dir string) config.Config {
	t.Helper()
	if dir != "" {
		sitetest.Write(t, dir)
	}
	return config.Config{Version: 1, Dir: dir, Targets: []config.Target{
		{Name: "a", Provider: "site", Path: "dist", BaseURL: sitetest.Base},
		{Name: "b", Provider: "site", Path: "dist", BaseURL: sitetest.Base, WriteDir: "public", LLMsTxt: &config.LLMsTxt{Title: "Example"}},
	}}
}

func TestPlanApplyVerify(t *testing.T) {
	ctx := context.Background()
	e, err := New(cfg(t, t.TempDir()), Default(), []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.Plan(ctx)
	if err != nil || len(p.Actions) != 1 || p.Actions[0].Target != "b" || p.Actions[0].Resource != "llms.txt" {
		t.Fatalf("plan %+v %v", p, err)
	}
	var out bytes.Buffer
	if err := e.Apply(ctx, p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "applied and verified") {
		t.Fatalf("out %q", out.String())
	}
	if st := e.Status(ctx); len(st) != 1 || st[0].Summary != "2 pages, 0 errors, 0 warnings, 0 notes" {
		t.Fatalf("status %+v", st)
	}
}

func TestApplyCatchesAWriteThatDidNothing(t *testing.T) {
	ctx := context.Background()
	c := cfg(t, t.TempDir())
	c.Targets[0].Provider = "stuck"
	e, _ := New(c, Providers{"stuck": stuck{}}, []string{"a"})
	p, _ := e.Plan(ctx)
	if err := e.Apply(ctx, p, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "still outstanding") {
		t.Fatalf("apply %v", err)
	}
}

func TestSelectAndMissingProvider(t *testing.T) {
	if _, err := New(cfg(t, ""), Default(), []string{"nope"}); err == nil {
		t.Fatal("unknown target accepted")
	}
	e, _ := New(cfg(t, ""), Providers{}, nil)
	if _, err := e.Plan(context.Background()); err == nil {
		t.Fatal("missing provider planned")
	}
	if st := e.Status(context.Background()); st[0].Error == "" {
		t.Fatalf("status %+v", st)
	}
}
