package arbiter

import (
	"strings"
)

var jailbreakPatterns = []string{
	"ignore previous instructions",
	"ignore your instructions",
	"ignore all instructions",
	"ignore the above",
	"disregard previous",
	"disregard your instructions",
	"disregard all",
	"forget your instructions",
	"forget previous",
	"you are now",
	"pretend you are",
	"act as if you",
	"new instructions",
	"override your",
	"system prompt",
	"list all rules",
	"list every rule",
	"list the rules",
	"show me the rules",
	"show all rules",
	"tell me all the rules",
	"tell me every rule",
	"what are all the rules",
	"give me the rules",
	"give me all rules",
	"enumerate the rules",
	"full text of",
	"complete text of",
	"verbatim text",
	"reproduce the rule",
	"print the rule",
	"output your prompt",
	"reveal your prompt",
	"show your prompt",
	"what is your prompt",
	"what are your instructions",
	"repeat your instructions",
	"dan mode",
	"developer mode",
	"jailbreak",
}

func checkInputFilters(input string) (bool, string) {
	lower := strings.ToLower(input)
	for _, pattern := range jailbreakPatterns {
		if strings.Contains(lower, pattern) {
			return true, "jailbreak pattern: " + pattern
		}
	}
	return false, ""
}

func checkOutputFilters(output string) bool {
	lower := strings.ToLower(output)

	// Check for signs the model is dumping rule text
	ruleHeaders := []string{
		"recorded: thursday, april",
		"recorded: october",
		"recorded: february",
		"recorded: june",
		"recorded: july",
		"recorded: august",
		"recorded: january",
		"recorded: march",
		"recorded: november",
		"recorded: december",
		"recorded: september",
		"recorded: may",
	}

	citationCount := 0
	for _, header := range ruleHeaders {
		if strings.Contains(lower, header) {
			citationCount++
		}
	}
	// If the output contains 3+ citation-style lines, it's likely dumping rules
	if citationCount >= 3 {
		return true
	}

	// Check for suspiciously long verbatim passages
	verbatimMarkers := []string{
		"participants are permitted to pick up pennies under the following conditions",
		"a participant is permitted to kick a tails-up coin",
		"children, twelve and under, are exempt from these rules",
		"if there are two pennies laying on the sidewalk",
		"the writer of this paper does not believe in superstition",
	}
	matchCount := 0
	for _, marker := range verbatimMarkers {
		if strings.Contains(lower, marker) {
			matchCount++
		}
	}
	if matchCount >= 2 {
		return true
	}

	return false
}
