CREATE TABLE firewall_plans (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    body TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    operation_id TEXT UNIQUE
);
CREATE TABLE operations (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    plan_id TEXT UNIQUE REFERENCES firewall_plans(id),
    idempotency_key TEXT UNIQUE,
    action TEXT NOT NULL,
    subject TEXT NOT NULL,
    actor TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    record TEXT NOT NULL,
    terminal INTEGER NOT NULL DEFAULT 0,
    pending_sync INTEGER NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX one_node_operation ON operations(node_id) WHERE terminal=0;
