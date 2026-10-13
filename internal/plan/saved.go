package plan

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// A Saved plan is what was reviewed, written down. It records intent rather
// than closures: on apply the tool re-plans from the same config and refuses
// unless the fresh plan is identical, so a world that moved in between fails
// loudly instead of quietly running something nobody saw.
type Saved struct {
	Version int `json:"version"`
	// Targets is the -target selection the plan was made with; empty means
	// every target.
	Targets []string      `json:"targets,omitempty"`
	Actions []SavedAction `json:"actions"`
}

// SavedAction is one line of a reviewed plan.
type SavedAction struct {
	Op       string `json:"op"`
	Target   string `json:"target"`
	Resource string `json:"resource"`
	Detail   string `json:"detail"`
}

const savedVersion = 1

// Snapshot is the plan as reviewed, made for the given targets.
func (p Plan) Snapshot(targets []string) Saved {
	return Saved{Version: savedVersion, Targets: targets, Actions: p.savedActions()}
}

// Save writes the plan, made for the given targets, for a later apply.
func (p Plan) Save(w io.Writer, targets []string) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p.Snapshot(targets))
}

func (p Plan) savedActions() []SavedAction {
	out := make([]SavedAction, 0, len(p.Actions))
	for _, a := range p.sorted() {
		out = append(out, SavedAction{Op: string(a.Op), Target: a.Target, Resource: a.Resource, Detail: a.Detail})
	}
	return out
}

// LoadSaved reads a plan written by Save.
func LoadSaved(r io.Reader) (Saved, error) {
	var s Saved
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return Saved{}, fmt.Errorf("read saved plan: %w", err)
	}
	if s.Version != savedVersion {
		return Saved{}, fmt.Errorf("saved plan is version %d, this build writes version %d", s.Version, savedVersion)
	}
	return s, nil
}

// Matches reports whether a fresh plan is the one that was reviewed. Every
// field counts, the detail included: it says why, and a reviewer who approved
// one reason did not approve another.
func (s Saved) Matches(fresh Plan) error {
	now := fresh.savedActions()
	if len(now) != len(s.Actions) {
		return fmt.Errorf("the live state changed since this plan was saved: it had %d action(s), there are now %d; re-run plan",
			len(s.Actions), len(now))
	}
	for i := range now {
		if now[i] != s.Actions[i] {
			return fmt.Errorf("the live state changed since this plan was saved.\n  reviewed: %s\n  now:      %s\nre-run plan",
				describe(s.Actions[i]), describe(now[i]))
		}
	}
	return nil
}

func describe(a SavedAction) string {
	return strings.TrimSpace(fmt.Sprintf("%s %s %s: %s", a.Op, a.Target, a.Resource, a.Detail))
}
