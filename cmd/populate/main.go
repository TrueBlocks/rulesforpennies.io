package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

type Rule struct {
	Code         string
	Title        string
	FullText     string
	Summary      string
	Keywords     string
	Category     string
	RecordedDate string
	Foundational bool
}

var recordedRe = regexp.MustCompile(`— Recorded:\s*(.+)`)

type section struct {
	name     string
	category string
	counter  int
}

func main() {
	rulesFile := flag.String("rules", "", "path to Rules for Pennies.md")
	dbFile := flag.String("db", "", "path to output rules.db")
	schemaFile := flag.String("schema", "", "path to schema.sql")
	flag.Parse()

	if *rulesFile == "" || *dbFile == "" || *schemaFile == "" {
		log.Fatal("usage: populate -rules <file> -db <file> -schema <file>")
	}

	raw, err := os.ReadFile(*rulesFile)
	if err != nil {
		log.Fatalf("reading rules: %v", err)
	}

	schemaSQL, err := os.ReadFile(*schemaFile)
	if err != nil {
		log.Fatalf("reading schema: %v", err)
	}

	rules := parseRules(string(raw))

	os.Remove(*dbFile)
	db, err := sql.Open("sqlite", *dbFile)
	if err != nil {
		log.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(string(schemaSQL)); err != nil {
		log.Fatalf("creating schema: %v", err)
	}

	stmt, err := db.Prepare(`INSERT INTO rules (rule_code, title, full_text, summary, keywords, category, recorded_date, is_foundational)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		log.Fatalf("preparing insert: %v", err)
	}
	defer stmt.Close()

	for _, r := range rules {
		foundational := 0
		if r.Foundational {
			foundational = 1
		}
		_, err := stmt.Exec(r.Code, r.Title, r.FullText, r.Summary, r.Keywords, r.Category, r.RecordedDate, foundational)
		if err != nil {
			log.Fatalf("inserting %s: %v", r.Code, err)
		}
	}

	fmt.Printf("Inserted %d rules into %s\n", len(rules), *dbFile)
}

func parseRules(text string) []Rule {
	// Split on double blank lines (two or more consecutive newlines with only whitespace)
	blocks := splitBlocks(text)

	var rules []Rule

	sections := map[string]*section{
		"front":        {name: "Front Matter", category: "front", counter: 0},
		"definitions":  {name: "Definitions", category: "definitions", counter: 0},
		"foundational": {name: "Foundational Rules", category: "foundational", counter: 0},
		"situational":  {name: "Situational Rules", category: "situational", counter: 0},
		"commerce":     {name: "Commerce Rules", category: "commerce", counter: 0},
		"location":     {name: "Location & Environment", category: "location", counter: 0},
		"special":      {name: "Special Cases", category: "special", counter: 0},
		"governance":   {name: "Governance & Back Matter", category: "governance", counter: 0},
	}

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		title, body := extractTitle(block)
		if title == "" {
			continue
		}

		cat := classifyRule(title, body)
		sec := sections[cat]
		sec.counter++

		sectionNum := sectionNumber(cat)
		code := fmt.Sprintf("§%d.%d", sectionNum, sec.counter)

		recordedDate := ""
		if m := recordedRe.FindStringSubmatch(body); len(m) > 1 {
			recordedDate = strings.TrimSpace(m[1])
		}

		foundational := cat == "foundational"

		rules = append(rules, Rule{
			Code:         code,
			Title:        title,
			FullText:     body,
			Summary:      generateSummary(title, body),
			Keywords:     generateKeywords(title, body),
			Category:     cat,
			RecordedDate: recordedDate,
			Foundational: foundational,
		})
	}

	return rules
}

func splitBlocks(text string) []string {
	// Split on two or more consecutive blank lines
	re := regexp.MustCompile(`\n\s*\n\s*\n`)
	return re.Split(text, -1)
}

func extractTitle(block string) (string, string) {
	lines := strings.SplitN(block, "\n", 2)
	title := strings.TrimSpace(lines[0])

	// Skip very short "titles" that are continuations
	if len(title) < 3 {
		return "", ""
	}

	// Skip lines that start with lowercase (continuation paragraphs)
	if len(title) > 0 && title[0] >= 'a' && title[0] <= 'z' {
		return "", ""
	}

	body := ""
	if len(lines) > 1 {
		body = strings.TrimSpace(lines[1])
	}

	return title, body
}

func sectionNumber(cat string) int {
	switch cat {
	case "front":
		return 0
	case "definitions":
		return 1
	case "foundational":
		return 2
	case "situational":
		return 3
	case "commerce":
		return 4
	case "location":
		return 5
	case "special":
		return 6
	case "governance":
		return 7
	default:
		return 9
	}
}

func classifyRule(title, body string) string {
	tl := strings.ToLower(title)

	// Front matter
	frontTitles := []string{"introduction", "objectives of the discussion", "format of the presentation"}
	for _, ft := range frontTitles {
		if tl == ft {
			return "front"
		}
	}

	// Definitions section
	if tl == "playing field" || tl == "definitions" {
		return "definitions"
	}

	// Governance and back matter
	govTitles := []string{"governance and procedures", "conclusion", "issues under advisement",
		"items referred to subcommittee", "other titles from your publisher"}
	for _, gt := range govTitles {
		if tl == gt {
			return "governance"
		}
	}

	// Foundational rules
	foundTitles := []string{"heads up", "heads up with a kick", "accidental kicks",
		"picking up non-pennies", "unrecognizable coins", "children exemption", "two penny rule"}
	for _, ft := range foundTitles {
		if tl == ft {
			return "foundational"
		}
	}

	// Commerce rules
	commerceKeywords := []string{"store", "wawa", "change", "receipt", "market", "checkout",
		"purchase", "cashier", "penny rule", "must keeper", "two cents", "manufacturing"}
	for _, kw := range commerceKeywords {
		if strings.Contains(tl, kw) || strings.Contains(strings.ToLower(body[:min(200, len(body))]), kw) {
			return "commerce"
		}
	}

	// Location rules
	locationKeywords := []string{"bathroom", "table", "tarred", "road", "glove box",
		"wishing well", "foreign", "japan", "airport", "nickel rule"}
	for _, kw := range locationKeywords {
		if strings.Contains(tl, kw) {
			return "location"
		}
	}

	// Special cases (people, unusual situations)
	specialKeywords := []string{"fat lady", "old men", "meriam", "two person", "money handler",
		"fiduciary", "hole in pocket", "bare foot", "pandemic", "inversion",
		"there's always", "stubbornly", "trashing", "many coins", "leaving the scene",
		"gracious refusal"}
	for _, kw := range specialKeywords {
		if strings.Contains(tl, kw) {
			return "special"
		}
	}

	// Situational rules (state, time, determination)
	situationalKeywords := []string{"indeterminate", "time of possession", "sun factor",
		"drag along", "50 step", "gravity", "step rule"}
	for _, kw := range situationalKeywords {
		if strings.Contains(tl, kw) {
			return "situational"
		}
	}

	// Default: situational
	return "situational"
}

func generateSummary(title, body string) string {
	// Take the first sentence or two of the body as a simple summary
	sentences := splitSentences(body)
	if len(sentences) == 0 {
		return title
	}

	summary := sentences[0]
	if len(sentences) > 1 && len(summary)+len(sentences[1]) < 200 {
		summary += " " + sentences[1]
	}

	// Remove the "— Recorded" line if it snuck in
	if idx := strings.Index(summary, "— Recorded"); idx > 0 {
		summary = strings.TrimSpace(summary[:idx])
	}

	return summary
}

func splitSentences(text string) []string {
	// Simple sentence splitter
	re := regexp.MustCompile(`[.!?]\s+`)
	parts := re.Split(text, -1)
	var result []string
	indices := re.FindAllStringIndex(text, -1)

	start := 0
	for _, idx := range indices {
		s := strings.TrimSpace(text[start:idx[1]])
		if s != "" {
			result = append(result, s)
		}
		start = idx[1]
	}
	// Last part
	if start < len(text) {
		s := strings.TrimSpace(text[start:])
		if s != "" && !strings.HasPrefix(s, "— Recorded") {
			result = append(result, s)
		}
	}

	if len(result) == 0 && len(parts) > 0 {
		return parts[:1]
	}
	return result
}

func generateKeywords(title, body string) string {
	// Extract meaningful keywords from title and body
	keywords := []string{}

	// Title words (excluding common words)
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "of": true, "for": true,
		"in": true, "on": true, "to": true, "and": true, "or": true,
		"is": true, "it": true, "if": true, "with": true, "that": true,
		"rule": true, "rules": true,
	}

	for _, word := range strings.Fields(strings.ToLower(title)) {
		cleaned := strings.Trim(word, ".,;:!?\"'()-")
		if len(cleaned) > 2 && !stopWords[cleaned] {
			keywords = append(keywords, cleaned)
		}
	}

	// Add penny-related terms found in body
	pennyTerms := []string{"heads", "tails", "kick", "pick up", "penny", "coin",
		"flip", "drop", "encounter", "indeterminate", "surface", "luck",
		"parking lot", "sidewalk", "store", "wawa", "bathroom", "pocket",
		"foreign", "urine", "old", "children", "witness"}
	bodyLower := strings.ToLower(body)
	for _, term := range pennyTerms {
		if strings.Contains(bodyLower, term) {
			keywords = append(keywords, term)
		}
	}

	// Deduplicate
	seen := map[string]bool{}
	var unique []string
	for _, kw := range keywords {
		if !seen[kw] {
			seen[kw] = true
			unique = append(unique, kw)
		}
	}

	return strings.Join(unique, ", ")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
