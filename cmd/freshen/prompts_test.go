package main

import (
	"strings"
	"testing"
)

func TestRenderRulePromptFillsTitleAndBody(t *testing.T) {
	r := parsedRule{Title: "The Sun Factor", FullText: "A penny found in sunlight is worth two in shade."}

	summary, err := renderRulePrompt(summaryPrompt, r)
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

	keywords, err := renderRulePrompt(keywordsPrompt, r)
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

func TestRuleConstPromptsCarryTheirSlots(t *testing.T) {
	for name, tmpl := range map[string]string{"summary": summaryPrompt, "keywords": keywordsPrompt} {
		if !strings.Contains(tmpl, "{{.Title}}") || !strings.Contains(tmpl, "{{.Body}}") {
			t.Errorf("%s prompt is missing a title or body slot", name)
		}
	}
}
