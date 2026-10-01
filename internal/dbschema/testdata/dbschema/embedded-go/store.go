package store

const schema = `
CREATE TABLE IF NOT EXISTS symbols (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    kind TEXT NOT NULL
);

CREATE INDEX idx_symbols_name ON symbols(name);
`
