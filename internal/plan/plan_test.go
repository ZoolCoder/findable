package plan

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sample() Plan {
	var p Plan
	p.Add(
		Action{Op: OpUpdate, Target: "b", Resource: "x.txt", Detail: "content differs"},
		Action{Op: OpDelete, Target: "a", Resource: "old.txt", Detail: "not in the config"},
		Action{Op: OpManual, Target: "a", Resource: "secret.txt", Detail: "yours to write"},
	)
	return p
}

func TestWriteIsSortedAndAligned(t *testing.T) {
	var out bytes.Buffer
	if err := sample().Write(&out); err != nil {
		t.Fatal(err)
	}
	want := "DELETE  a  old.txt     not in the config\n" +
		"MANUAL  a  secret.txt  yours to write\n" +
		"UPDATE  b  x.txt       content differs\n" +
		"\n1 of these cannot be automated and are yours to do.\n"
	if out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", out.String(), want)
	}
	out.Reset()
	_ = Plan{}.Write(&out)
	if !strings.HasPrefix(out.String(), "no changes") {
		t.Fatalf("empty plan: %q", out.String())
	}
}

func TestWriteJSONAndPredicates(t *testing.T) {
	p := sample()
	if !p.Destructive() || len(p.Executable()) != 2 || len(p.Manual()) != 1 || p.Empty() {
		t.Fatal("predicates wrong")
	}
	var out bytes.Buffer
	if err := p.WriteJSON(&out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Changes, Manual int
		Actions         []JSONAction
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Changes != 2 || got.Manual != 1 || got.Actions[0].Resource != "old.txt" {
		t.Fatalf("json %+v", got)
	}
}

func TestSavedMatches(t *testing.T) {
	var buf bytes.Buffer
	if err := sample().Save(&buf, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	saved, err := LoadSaved(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Targets[0] != "a" {
		t.Fatalf("targets %v", saved.Targets)
	}
	if err := saved.Matches(sample()); err != nil {
		t.Fatal(err)
	}
	moved := sample()
	moved.Actions[0].Detail = "content differs again"
	if err := saved.Matches(moved); err == nil || !strings.Contains(err.Error(), "reviewed:") {
		t.Fatalf("moved plan: %v", err)
	}
	if err := saved.Matches(Plan{}); err == nil {
		t.Fatal("empty plan matched")
	}
	if _, err := LoadSaved(strings.NewReader(`{"version":9}`)); err == nil {
		t.Fatal("future version accepted")
	}
}

func TestWriteDropsControlCharactersFromDetail(t *testing.T) {
	var p Plan
	p.Add(Action{Op: OpManual, Target: "a", Resource: "r", Detail: "ok\nMANUAL forged\x1b[31m\ttab"})
	var b bytes.Buffer
	if err := p.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\tt") || strings.Contains(out, "ok\nMANUAL") {
		t.Fatalf("control characters survived: %q", out)
	}
}
