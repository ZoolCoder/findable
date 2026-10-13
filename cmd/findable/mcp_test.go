package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPPlanThenApplyByID(t *testing.T) {
	cfg, dir := fixture(t)
	s := mcpServer(t.TempDir())
	text, err := s.Call("plan", map[string]any{"config": cfg})
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		ID   string `json:"plan_id"`
		Plan struct{ Changes int }
	}
	if err := json.Unmarshal([]byte(text), &reply); err != nil || reply.Plan.Changes != 2 {
		t.Fatalf("plan %q: %v", text, err)
	}
	if text, err = s.Call("apply", map[string]any{"plan_id": reply.ID}); err != nil || !strings.Contains(text, "applied and verified") {
		t.Fatalf("apply %q: %v", text, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "llms.txt")); err != nil {
		t.Fatal(err)
	}
	if text, err := s.Call("status", map[string]any{"config": cfg}); err != nil || !strings.Contains(text, `"target": "example"`) {
		t.Fatalf("status %q: %v", text, err)
	}
}

func TestMCPAudit(t *testing.T) {
	cfg, _ := fixture(t)
	text, err := mcpServer(t.TempDir()).Call("audit", map[string]any{"config": cfg})
	if err != nil || !strings.Contains(text, `"check": "ai.crawler.allowed"`) {
		t.Fatalf("audit %q: %v", text, err)
	}
}
