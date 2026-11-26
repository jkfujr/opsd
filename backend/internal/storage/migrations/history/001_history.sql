CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,name TEXT NOT NULL,applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')));

CREATE TABLE history_meta (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  node_id TEXT NOT NULL,
  import_through INTEGER,
  mirror_through INTEGER
) STRICT;

CREATE TABLE history_samples (
  collected_at INTEGER PRIMARY KEY CHECK (collected_at >= 0),
  body BLOB NOT NULL CHECK (
    length(body) BETWEEN 2 AND 1024
    AND json_valid(CAST(body AS TEXT))
  )
) STRICT;
