CREATE TABLE account (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    entrance TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE control_identity (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    private_key BLOB NOT NULL,
    public_key BLOB NOT NULL
);
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    csrf_token TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    remote_address TEXT NOT NULL,
    revoked_at INTEGER
);
CREATE TABLE nodes (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    public_key TEXT NOT NULL UNIQUE,
    local INTEGER NOT NULL DEFAULT 0,
    revoked_at INTEGER,
    last_seen_at INTEGER,
    capabilities TEXT NOT NULL
);
CREATE UNIQUE INDEX one_local_node ON nodes(local) WHERE local = 1;
CREATE TABLE enrollment_tokens (
    id TEXT PRIMARY KEY,
    token_hash BLOB NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER,
    revoked_at INTEGER
);
