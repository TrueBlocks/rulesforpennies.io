package arbiter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TrueBlocks/rulesforpennies.io/internal/rulesdb"
)

func TestBuildPromptInjectsRulesCorpus(t *testing.T) {
	svc := New(nil, "PERSONA\n\n{{.RulesCorpus}}\n\nEND", nil, nil, nil)
	out, err := svc.buildPrompt([]rulesdb.Rule{
		{Code: "§3.4", Title: "The Sun Factor", FullText: "A penny in sunlight is worth two in shade."},
	})
	if err != nil {
		t.Fatalf("building prompt: %v", err)
	}
	for _, want := range []string{"PERSONA", "END", "--- §3.4 The Sun Factor ---", "A penny in sunlight is worth two in shade."} {
		if !strings.Contains(out, want) {
			t.Errorf("built prompt is missing %q", want)
		}
	}
	if strings.Contains(out, "{{") {
		t.Error("the rules-corpus slot was not filled")
	}
}

func TestArbiterSystemPromptCarriesPersonaAndSlot(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "cmd", "arbiterd", "system-prompt.txt"))
	if err != nil {
		t.Fatalf("reading the arbiter system prompt: %v", err)
	}
	prompt := string(data)
	for _, want := range []string{"{{.RulesCorpus}}", "the arbiter finds", "deadpan"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt is missing %q", want)
		}
	}
}
