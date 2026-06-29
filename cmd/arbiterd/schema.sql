CREATE TABLE IF NOT EXISTS rules (
    id INTEGER PRIMARY KEY,
    rule_code TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL,
    full_text TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    keywords TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    recorded_date TEXT,
    is_foundational INTEGER NOT NULL DEFAULT 0
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
