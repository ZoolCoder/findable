// Package engine turns a config plus live state into one plan, applies it,
// and re-plans to verify the apply converged.
package engine

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/zoolcoder/findable/internal/check"
	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/findable/internal/provider"
	"github.com/zoolcoder/findable/internal/provider/website"
)

// Providers maps a config provider name to its implementation.
type Providers map[string]provider.Provider

// Default is every built-in provider.
func Default() Providers {
	return Providers{"site": website.New()}
}

type Engine struct {
	cfg       config.Config
	providers Providers
	targets   []config.Target
}

// New selects the named targets, or every target when names is empty.
func New(cfg config.Config, providers Providers, names []string) (*Engine, error) {
	targets, err := Select(cfg, names)
	if err != nil {
		return nil, err
	}
	return &Engine{cfg: cfg, providers: providers, targets: targets}, nil
}

// Select returns the named targets with their paths resolved, in config
// order. An unknown name is an error, not an empty plan.
func Select(cfg config.Config, names []string) ([]config.Target, error) {
	for _, n := range names {
		if _, ok := cfg.Find(n); !ok {
			return nil, fmt.Errorf("no target named %q in the config", n)
		}
	}
	var out []config.Target
	for _, t := range cfg.Targets {
		if len(names) == 0 || slices.Contains(names, t.Name) {
			t.Path = cfg.Root(t)
			t.WriteDir = cfg.Resolve(t.WriteDir)
			out = append(out, t)
		}
	}
	return out, nil
}

func (e *Engine) provider(t config.Target) (provider.Provider, error) {
	p, ok := e.providers[t.Provider]
	if !ok {
		return nil, fmt.Errorf("target %s: provider %q is not available in this build", t.Name, t.Provider)
	}
	return p, nil
}

// Plan reads live state for every selected target.
func (e *Engine) Plan(ctx context.Context) (plan.Plan, error) {
	var p plan.Plan
	for _, t := range e.targets {
		prov, err := e.provider(t)
		if err != nil {
			return plan.Plan{}, err
		}
		actions, err := prov.Plan(ctx, t)
		if err != nil {
			return plan.Plan{}, fmt.Errorf("plan %s: %w", t.Name, err)
		}
		p.Add(actions...)
	}
	return p, nil
}

// Apply runs every executable action in order and stops at the first
// failure. Then it re-plans: an action that reported success but left the
// target still different is a failure too.
func (e *Engine) Apply(ctx context.Context, p plan.Plan, w io.Writer) error {
	for _, a := range p.Executable() {
		if err := a.Do(ctx); err != nil {
			return fmt.Errorf("%s %s %s: %w", a.Op, a.Target, a.Resource, err)
		}
		fmt.Fprintf(w, "done  %s %s %s\n", a.Op, a.Target, a.Resource)
	}
	after, err := e.Plan(ctx)
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if left := after.Executable(); len(left) > 0 {
		return fmt.Errorf("applied, but %d change(s) are still outstanding; run plan to see them", len(left))
	}
	_, err = fmt.Fprintln(w, "applied and verified")
	return err
}

// TargetStatus is one target's live summary.
type TargetStatus struct {
	Target   string `json:"target"`
	Provider string `json:"provider"`
	Summary  string `json:"summary"`
	Error    string `json:"error,omitempty"`
}

// Status summarises every selected target. A target that cannot be read is
// reported in its row rather than failing the others.
func (e *Engine) Status(ctx context.Context) []TargetStatus {
	out := make([]TargetStatus, 0, len(e.targets))
	for _, t := range e.targets {
		row := TargetStatus{Target: t.Name, Provider: t.Provider}
		prov, err := e.provider(t)
		if err == nil {
			row.Summary, err = prov.Status(ctx, t)
		}
		if err != nil {
			row.Error = err.Error()
		}
		out = append(out, row)
	}
	return out
}

// Auditor is a provider that can list its findings with every page named,
// beyond the one line per finding a plan shows.
type Auditor interface {
	Audit(ctx context.Context, t config.Target) ([]check.Finding, error)
}

// TargetFindings is one target's audit.
type TargetFindings struct {
	Target   string          `json:"target"`
	Findings []check.Finding `json:"findings"`
	Error    string          `json:"error,omitempty"`
}

// Audit lists every selected target's findings. Like Status, a target that
// cannot be read is reported in its row.
func (e *Engine) Audit(ctx context.Context) []TargetFindings {
	out := make([]TargetFindings, 0, len(e.targets))
	for _, t := range e.targets {
		row := TargetFindings{Target: t.Name, Findings: []check.Finding{}}
		prov, err := e.provider(t)
		if err == nil {
			a, ok := prov.(Auditor)
			if !ok {
				err = fmt.Errorf("provider %q has no audit", t.Provider)
			} else if row.Findings, err = a.Audit(ctx, t); row.Findings == nil {
				row.Findings = []check.Finding{}
			}
		}
		if err != nil {
			row.Error = err.Error()
		}
		out = append(out, row)
	}
	return out
}
