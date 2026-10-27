CREATE TABLE operations (
    id TEXT PRIMARY KEY,
    action TEXT,
    contract_version INTEGER NOT NULL,
    record TEXT NOT NULL,
    recipe TEXT,
	component_state TEXT,
    terminal INTEGER NOT NULL DEFAULT 0,
    boot_id TEXT NOT NULL DEFAULT '',
    deadline_ns INTEGER NOT NULL DEFAULT 0,
    next_step INTEGER NOT NULL DEFAULT 0,
    intent_step INTEGER,
    request TEXT NOT NULL DEFAULT '',
    request_revision INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX one_local_operation ON operations(terminal) WHERE terminal=0;
