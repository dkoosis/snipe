package cclog

const cclogSchema = `
CREATE TABLE sessions (
    id   TEXT PRIMARY KEY,
    started_at INT NOT NULL
);

CREATE TABLE messages (
    id   TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    body TEXT
);
`
