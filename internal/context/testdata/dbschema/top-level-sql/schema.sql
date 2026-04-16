-- test schema
CREATE TABLE queue (
  path      TEXT NOT NULL,
  path_hash TEXT NOT NULL,
  PRIMARY KEY (path_hash)
);

CREATE INDEX idx_queue_path ON queue(path);
