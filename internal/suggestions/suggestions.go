package suggestions

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Ruling struct {
	ID         int             `json:"id"`
	Slug       string          `json:"slug"`
	Situation  string          `json:"situation"`
	RulingTxt  string          `json:"ruling"`
	Rules      json.RawMessage `json:"rules"`
	IsProposed bool            `json:"is_proposed"`
	Rationale  string          `json:"rationale,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening store db: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS rulings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slug TEXT NOT NULL,
		situation TEXT NOT NULL,
		ruling TEXT NOT NULL,
		rules_json TEXT NOT NULL DEFAULT '[]',
		is_proposed INTEGER NOT NULL DEFAULT 0,
		rationale TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL
	)`); err != nil {
		return nil, fmt.Errorf("creating rulings table: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveRuling(slug, situation, ruling string, rulesJSON []byte) error {
	s.db.Exec(`DELETE FROM rulings WHERE slug = ?`, slug)
	_, err := s.db.Exec(
		`INSERT INTO rulings (slug, situation, ruling, rules_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		slug, situation, ruling, string(rulesJSON), time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) List() ([]Ruling, error) {
	rows, err := s.db.Query(`SELECT id, slug, situation, ruling, rules_json, is_proposed, rationale, created_at FROM rulings ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Ruling
	for rows.Next() {
		var r Ruling
		var rulesStr string
		var proposed int
		if err := rows.Scan(&r.ID, &r.Slug, &r.Situation, &r.RulingTxt, &rulesStr, &proposed, &r.Rationale, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Rules = json.RawMessage(rulesStr)
		r.IsProposed = proposed == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Propose(slug, rationale string) error {
	_, err := s.db.Exec(`UPDATE rulings SET is_proposed = 1, rationale = ? WHERE slug = ?`, rationale, slug)
	return err
}

func (s *Store) Delete(id int) error {
	_, err := s.db.Exec(`DELETE FROM rulings WHERE id = ?`, id)
	return err
}
