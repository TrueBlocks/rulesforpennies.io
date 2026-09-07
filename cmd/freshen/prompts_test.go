package main

import (
	"strings"
	"testing"

	cooking "github.com/TrueBlocks/trueblocks-art/packages/prompt"
)

func TestRulePromptsFillTitleAndBody(t *testing.T) {
	t.Setenv("TRUEBLOCKS_DATA_DIR", t.TempDir())
	r := parsedRule{Title: "The Sun Factor", FullText: "A penny found in sunlight is worth two in shade."}
	data := struct{ Title, Body string }{r.Title, r.FullText}

	summary, err := summaryPrompt.Fill(data)
	if err != nil {
		t.Fatalf("rendering summary prompt: %v", err)
	}
	if !strings.Contains(summary, "The Sun Factor") || !strings.Contains(summary, "A penny found in sunlight") {
		t.Error("summary prompt did not receive the rule's title and body")
	}
	if strings.Contains(summary, "{{") {
		t.Error("summary prompt still has unfilled slots")
	}
	if !strings.Contains(summary, "one-sentence summary") {
		t.Error("summary prompt lost its instruction")
	}

	keywords, err := keywordsPrompt.Fill(data)
	if err != nil {
		t.Fatalf("rendering keywords prompt: %v", err)
	}
	if !strings.Contains(keywords, "The Sun Factor") || !strings.Contains(keywords, "A penny found in sunlight") {
		t.Error("keywords prompt did not receive the rule's title and body")
	}
	if !strings.Contains(keywords, "comma-separated") {
		t.Error("keywords prompt lost its instruction")
	}
}

func TestRulePromptsCarryTheirSlots(t *testing.T) {
	for name, p := range map[string]*cooking.Prompt{"summary": summaryPrompt, "keywords": keywordsPrompt} {
		if !strings.Contains(p.Text(), "{{.Title}}") || !strings.Contains(p.Text(), "{{.Body}}") {
			t.Errorf("%s prompt is missing a title or body slot", name)
		}
	}
}
