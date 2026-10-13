// Package provider is the seam between the engine and a live system. A
// provider reads live state and returns the actions that would make it match
// a target; each action's Do closure carries the write.
//
// Add a provider: implement Provider in a package under internal/provider,
// add its name to config.Providers, and register it in engine.Default.
package provider

import (
	"context"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/plan"
)

// Provider reconciles one kind of target. The engine hands it a target whose
// Path is already resolved against the config file's directory.
type Provider interface {
	// Plan compares live state with t and returns what would change. It must
	// not write.
	Plan(ctx context.Context, t config.Target) ([]plan.Action, error)
	// Status is a one-line summary of live state, without diffing.
	Status(ctx context.Context, t config.Target) (string, error)
}
