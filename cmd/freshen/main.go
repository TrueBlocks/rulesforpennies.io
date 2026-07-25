package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/TrueBlocks/trueblocks-art/packages/ai"
	_ "modernc.org/sqlite"
)

const rulesSchema = `
CREATE TABLE IF NOT EXISTS rules (
    id INTEGER PRIMARY KEY,
    rule_code TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    full_text TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    keywords TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    recorded_date TEXT,
    is_foundational INTEGER NOT NULL DEFAULT 0,
    is_section_header INTEGER NOT NULL DEFAULT 0,
    content_hash TEXT NOT NULL DEFAULT ''
);

CREATE VIRTUAL TABLE IF NOT EXISTS rules_fts USING fts5(
    title,
    full_text,
    summary,
    keywords,
    content='rules',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS rules_ai AFTER INSERT ON rules BEGIN
    INSERT INTO rules_fts(rowid, title, full_text, summary, keywords)
    VALUES (new.id, new.title, new.full_text, new.summary, new.keywords);
END;

CREATE TRIGGER IF NOT EXISTS rules_ad AFTER DELETE ON rules BEGIN
    INSERT INTO rules_fts(rules_fts, rowid, title, full_text, summary, keywords)
    VALUES ('delete', old.id, old.title, old.full_text, old.summary, old.keywords);
END;

CREATE TRIGGER IF NOT EXISTS rules_au AFTER UPDATE ON rules BEGIN
    INSERT INTO rules_fts(rules_fts, rowid, title, full_text, summary, keywords)
    VALUES ('delete', old.id, old.title, old.full_text, old.summary, old.keywords);
    INSERT INTO rules_fts(rowid, title, full_text, summary, keywords)
    VALUES (new.id, new.title, new.full_text, new.summary, new.keywords);
END;
`

const summaryPrompt = `You are summarizing rules from a humorous rulebook about picking up pennies.

Given the following rule title and full text, write a single concise sentence that captures the operative condition or instruction of the rule. Ignore narrative flavor, anecdotes, examples, and recorded dates. Focus only on what a player must, may, or must not do.

Rule title: {{TITLE}}

Rule text:
{{BODY}}

Concise one-sentence summary:`

const keywordsPrompt = `You are extracting keywords from a rule in a humorous rulebook about picking up pennies.

Given the following rule title and full text, return a comma-separated list of 5–15 meaningful keywords or short phrases that would help searchers find this rule. Include important nouns, actions, locations, and concepts. Do not include common stop words.

Rule title: {{TITLE}}

Rule text:
{{BODY}}

Comma-separated keywords:`

var (
	majorSectionRe     = regexp.MustCompile(`^(\d+)\.00\s+(.+)$`)
	frontSubsectionRe  = regexp.MustCompile(`^0\.0([1-9])\s+(.+)$`)
	ruleHeaderRe       = regexp.MustCompile(`^(\d+)\.(\d+)\s+(.+)$`)
	imagePlaceholderRe = regexp.MustCompile(`^\[\[R:.+\]\]$`)
	recordedRe         = regexp.MustCompile(`^— Recorded:\s*(.+)$`)
)

type parsedRule struct {
	Code          string
	Title         string
	FullText      string
	RecordedDate  string
	Category      string
	SectionHeader bool
}

type cachedRule struct {
	Code          string
	Title         string
	FullText      string
	Summary       string
	Keywords      string
	Category      string
	RecordedDate  string
	Foundational  bool
	SectionHeader bool
	ContentHash   string
}

type stats struct {
	unchanged int
	updated   int
	new       int
	removed   int
	llmCalls  int
	cost      float64
}

func main() {
	rawFile := flag.String("raw", "", "path to raw Rules for Pennies markdown")
	dbFile := flag.String("db", "rules.db", "path to rules.db")
	providerName := flag.String("provider", "", "LLM provider: anthropic, openai, gemini, moonshot")
	model := flag.String("model", "", "LLM model")
	flag.Parse()

	if *rawFile == "" {
		log.Fatal("-raw is required")
	}

	cfg, err := ai.LoadSharedConfig()
	if err != nil {
		log.Fatalf("loading shared config: %v", err)
	}

	if *providerName == "" {
		*providerName = cfg.DefaultLLMProvider
	}
	if *model == "" {
		*model = cfg.DefaultLLMModel
	}
	if *providerName == "" {
		log.Fatal("provider is required (or set default_llm_provider in ~/.local/share/trueblocks/config.json)")
	}

	provider, err := newProvider(cfg, *providerName)
	if err != nil {
		log.Fatalf("creating provider: %v", err)
	}

	raw, err := os.ReadFile(*rawFile)
	if err != nil {
		log.Fatalf("reading raw file: %v", err)
	}

	rules := parseRules(string(raw))

	db, err := sql.Open("sqlite", *dbFile)
	if err != nil {
		log.Fatalf("opening db: %v", err)
	}
	defer db.Close()

	if err := migrateDB(db); err != nil {
		log.Fatalf("migrating db: %v", err)
	}

	s := &stats{}
	ctx := context.Background()

	for _, r := range rules {
		hash := contentHash(r.Title + "\n" + r.FullText)
		cached, err := findCached(db, r.Code)
		if err != nil {
			log.Fatalf("querying cache for %s: %v", r.Code, err)
		}

		if cached != nil && cached.ContentHash == hash {
			s.unchanged++
			fmt.Printf("%s %s — cached\n", r.Code, r.Title)
			continue
		}

		status := "new"
		if cached != nil {
			s.updated++
			status = "updated"
		} else {
			s.new++
		}
		fmt.Printf("%s %s — %s (summary)...", r.Code, r.Title, status)

		summary, cost, err := generate(ctx, provider, *model, summaryPrompt, r)
		if err != nil {
			log.Fatalf("generating summary for %s: %v", r.Code, err)
		}
		s.cost += cost
		s.llmCalls++

		fmt.Printf(" keywords...")
		keywords, cost, err := generate(ctx, provider, *model, keywordsPrompt, r)
		if err != nil {
			log.Fatalf("generating keywords for %s: %v", r.Code, err)
		}
		s.cost += cost
		s.llmCalls++

		if err := upsertRule(db, r, hash, summary, keywords); err != nil {
			log.Fatalf("upserting %s: %v", r.Code, err)
		}

		fmt.Printf(" done\n")
	}

	s.removed, err = deleteStaleRules(db, rules)
	if err != nil {
		log.Fatalf("deleting stale rules: %v", err)
	}

	printStats(s)
}

func parseRules(text string) []parsedRule {
	lines := strings.Split(text, "\n")
	var rules []parsedRule
	var current *parsedRule
	var currentBody []string
	var currentSection string

	flushCurrent := func() {
		if current == nil {
			return
		}
		current.FullText = strings.TrimSpace(strings.Join(currentBody, "\n"))
		rules = append(rules, *current)
		current = nil
		currentBody = nil
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		if m := majorSectionRe.FindStringSubmatch(line); m != nil {
			flushCurrent()
			currentSection = m[2]
			current = &parsedRule{
				Code:          fmt.Sprintf("§%s.00", m[1]),
				Title:         currentSection,
				Category:      categoryForSection(currentSection),
				SectionHeader: true,
			}
			continue
		}

		if m := frontSubsectionRe.FindStringSubmatch(line); m != nil {
			flushCurrent()
			currentSection = m[2]
			current = &parsedRule{
				Code:          fmt.Sprintf("§0.0%s", m[1]),
				Title:         currentSection,
				Category:      categoryForSection(currentSection),
				SectionHeader: true,
			}
			continue
		}

		if m := ruleHeaderRe.FindStringSubmatch(line); m != nil {
			flushCurrent()
			current = &parsedRule{
				Code:     fmt.Sprintf("§%s.%s", m[1], m[2]),
				Title:    m[3],
				Category: categoryForSection(currentSection),
			}
			continue
		}

		if current == nil {
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			currentBody = append(currentBody, "")
			continue
		}

		if imagePlaceholderRe.MatchString(trimmed) {
			continue
		}

		if m := recordedRe.FindStringSubmatch(trimmed); m != nil {
			current.RecordedDate = strings.TrimSpace(m[1])
			continue
		}

		currentBody = append(currentBody, line)
	}

	flushCurrent()
	return rules
}

func categoryForSection(title string) string {
	tl := strings.ToLower(title)
	switch {
	case strings.Contains(tl, "introduction") || strings.Contains(tl, "front"):
		return "front"
	case strings.Contains(tl, "playing field") || strings.Contains(tl, "definition"):
		return "definitions"
	case strings.Contains(tl, "foundational"):
		return "foundational"
	case strings.Contains(tl, "shopping") || strings.Contains(tl, "commerce"):
		return "commerce"
	case strings.Contains(tl, "location"):
		return "location"
	case strings.Contains(tl, "special"):
		return "special"
	case strings.Contains(tl, "governance") || strings.Contains(tl, "conclusion"):
		return "governance"
	default:
		return "situational"
	}
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func migrateDB(db *sql.DB) error {
	if _, err := db.Exec(rulesSchema); err != nil {
		return fmt.Errorf("creating schema: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE rules ADD COLUMN content_hash TEXT NOT NULL DEFAULT ''`); err != nil && !isDuplicateColumn(err) {
		return fmt.Errorf("adding content_hash: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE rules ADD COLUMN is_section_header INTEGER NOT NULL DEFAULT 0`); err != nil && !isDuplicateColumn(err) {
		return fmt.Errorf("adding is_section_header: %w", err)
	}

	rows, err := db.Query(`SELECT id, title, full_text FROM rules WHERE content_hash = ''`)
	if err != nil {
		return fmt.Errorf("querying missing hashes: %w", err)
	}

	type row struct {
		id       int
		title    string
		fullText string
	}
	var toUpdate []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.title, &r.fullText); err != nil {
			rows.Close()
			return fmt.Errorf("scanning row: %w", err)
		}
		toUpdate = append(toUpdate, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading rows: %w", err)
	}

	stmt, err := db.Prepare(`UPDATE rules SET content_hash = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("preparing hash update: %w", err)
	}
	defer stmt.Close()

	for _, r := range toUpdate {
		if _, err := stmt.Exec(contentHash(r.title+"\n"+r.fullText), r.id); err != nil {
			return fmt.Errorf("updating hash: %w", err)
		}
	}

	return nil
}

func isDuplicateColumn(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

func findCached(db *sql.DB, code string) (*cachedRule, error) {
	var r cachedRule
	var foundational, sectionHeader int
	err := db.QueryRow(`
		SELECT rule_code, title, full_text, summary, keywords, category, recorded_date, is_foundational, is_section_header, content_hash
		FROM rules WHERE rule_code = ?
	`, code).Scan(
		&r.Code, &r.Title, &r.FullText, &r.Summary, &r.Keywords,
		&r.Category, &r.RecordedDate, &r.Foundational, &r.SectionHeader, &r.ContentHash,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Foundational = foundational == 1
	r.SectionHeader = sectionHeader == 1
	return &r, nil
}

func generate(ctx context.Context, provider ai.Provider, model, promptTemplate string, r parsedRule) (string, float64, error) {
	prompt := strings.ReplaceAll(promptTemplate, "{{TITLE}}", r.Title)
	prompt = strings.ReplaceAll(prompt, "{{BODY}}", r.FullText)

	result, err := provider.Call(ctx, model, prompt, ai.CallOptions{
		MaxTokens: 300,
		Timeout:   2 * time.Minute,
	})
	if err != nil {
		return "", 0, err
	}

	return strings.TrimSpace(result.Content), result.Cost, nil
}

func upsertRule(db *sql.DB, r parsedRule, hash, summary, keywords string) error {
	foundational := 0
	if r.Category == "foundational" {
		foundational = 1
	}
	sectionHeader := 0
	if r.SectionHeader {
		sectionHeader = 1
	}

	_, err := db.Exec(`
		INSERT INTO rules (rule_code, title, full_text, summary, keywords, category, recorded_date, is_foundational, is_section_header, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(rule_code) DO UPDATE SET
			title = excluded.title,
			full_text = excluded.full_text,
			summary = excluded.summary,
			keywords = excluded.keywords,
			category = excluded.category,
			recorded_date = excluded.recorded_date,
			is_foundational = excluded.is_foundational,
			is_section_header = excluded.is_section_header,
			content_hash = excluded.content_hash
	`, r.Code, r.Title, r.FullText, summary, keywords, r.Category, r.RecordedDate, foundational, sectionHeader, hash)
	return err
}

func deleteStaleRules(db *sql.DB, rules []parsedRule) (int, error) {
	if len(rules) == 0 {
		return 0, nil
	}

	codes := make([]any, len(rules))
	for i, r := range rules {
		codes[i] = r.Code
	}

	placeholders := strings.Repeat("?,", len(codes)-1) + "?"
	result, err := db.Exec(fmt.Sprintf(`DELETE FROM rules WHERE rule_code NOT IN (%s)`, placeholders), codes...)
	if err != nil {
		return 0, err
	}

	n, _ := result.RowsAffected()
	return int(n), nil
}

func newProvider(cfg *ai.SharedConfig, name string) (ai.Provider, error) {
	switch name {
	case "anthropic":
		return cfg.NewAnthropic(), nil
	case "openai":
		return cfg.NewOpenAI(), nil
	case "gemini":
		return cfg.NewGemini(), nil
	case "moonshot":
		return cfg.NewMoonshot(), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s", name)
	}
}

func printStats(s *stats) {
	fmt.Printf("freshen complete: %d unchanged, %d updated, %d new, %d removed, %d LLM calls, $%.4f\n",
		s.unchanged, s.updated, s.new, s.removed, s.llmCalls, s.cost)
}
