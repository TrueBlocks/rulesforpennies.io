package rulesdb

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strings"

	_ "modernc.org/sqlite"
)

type Rule struct {
	ID           int
	Code         string
	Title        string
	FullText     string
	Summary      string
	Category     string
	Foundational bool
}

type DB struct {
	db *sql.DB
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening rules db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("pinging rules db: %w", err)
	}
	return &DB{db: db}, nil
}

func (d *DB) Close() error {
	return d.db.Close()
}

func (d *DB) Search(query string, limit int) ([]Rule, error) {
	terms := tokenize(query)
	if len(terms) == 0 {
		return d.Foundational()
	}

	ftsQuery := strings.Join(terms, " OR ")

	rows, err := d.db.Query(`
		SELECT r.id, r.rule_code, r.title, r.full_text, r.summary, r.category, r.is_foundational
		FROM rules r
		WHERE r.id IN (
			SELECT rowid FROM rules_fts WHERE rules_fts MATCH ?
		)
		ORDER BY r.is_foundational DESC
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("searching rules: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		var foundational int
		if err := rows.Scan(&r.ID, &r.Code, &r.Title, &r.FullText, &r.Summary, &r.Category, &foundational); err != nil {
			return nil, err
		}
		r.Foundational = foundational == 1
		rules = append(rules, r)
	}

	if len(rules) == 0 {
		return d.Foundational()
	}

	return rules, nil
}

func (d *DB) Foundational() ([]Rule, error) {
	rows, err := d.db.Query(`
		SELECT id, rule_code, title, full_text, summary, category, is_foundational
		FROM rules
		WHERE is_foundational = 1
		ORDER BY rule_code
	`)
	if err != nil {
		return nil, fmt.Errorf("fetching foundational rules: %w", err)
	}
	defer rows.Close()

	var rules []Rule
	for rows.Next() {
		var r Rule
		var foundational int
		if err := rows.Scan(&r.ID, &r.Code, &r.Title, &r.FullText, &r.Summary, &r.Category, &foundational); err != nil {
			return nil, err
		}
		r.Foundational = foundational == 1
		rules = append(rules, r)
	}
	return rules, nil
}

func (d *DB) Random(n int) ([]Rule, error) {
	rows, err := d.db.Query(`
		SELECT id, rule_code, title, full_text, summary, category, is_foundational
		FROM rules
		WHERE category NOT IN ('front', 'governance')
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []Rule
	for rows.Next() {
		var r Rule
		var foundational int
		if err := rows.Scan(&r.ID, &r.Code, &r.Title, &r.FullText, &r.Summary, &r.Category, &foundational); err != nil {
			return nil, err
		}
		r.Foundational = foundational == 1
		all = append(all, r)
	}

	if len(all) <= n {
		return all, nil
	}

	rand.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	return all[:n], nil
}

func tokenize(text string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "of": true, "for": true,
		"in": true, "on": true, "to": true, "and": true, "or": true,
		"is": true, "it": true, "if": true, "with": true, "that": true,
		"i": true, "my": true, "me": true, "we": true, "you": true,
		"am": true, "are": true, "was": true, "were": true, "be": true,
		"been": true, "being": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true,
		"can": true, "could": true, "should": true, "this": true, "what": true,
		"when": true, "where": true, "how": true, "not": true, "no": true,
		"but": true, "so": true, "at": true, "from": true, "up": true,
	}

	words := strings.Fields(strings.ToLower(text))
	var tokens []string
	for _, w := range words {
		cleaned := strings.Trim(w, ".,;:!?\"'()-")
		if len(cleaned) > 2 && !stopWords[cleaned] {
			tokens = append(tokens, cleaned)
		}
	}

	if len(tokens) > 10 {
		tokens = tokens[:10]
	}
	return tokens
}

func FormatForPrompt(rules []Rule) string {
	var b strings.Builder
	for _, r := range rules {
		fmt.Fprintf(&b, "--- %s %s ---\n%s\n\n", r.Code, r.Title, r.FullText)
	}
	return b.String()
}
