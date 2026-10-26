CREATE TABLE agent_identity (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    private_key BLOB NOT NULL,
    public_key BLOB NOT NULL,
    control_url TEXT NOT NULL,
    control_public_key TEXT NOT NULL,
    enrollment_token TEXT NOT NULL,
    node_id TEXT NOT NULL DEFAULT ''
);
