CREATE TABLE api_requests (
    actor TEXT NOT NULL,
    request_key TEXT NOT NULL,
    digest TEXT NOT NULL,
    kind TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    result_json TEXT NOT NULL,
    secret INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    PRIMARY KEY(actor, request_key)
);
ALTER TABLE console_theme_selection ADD COLUMN revision INTEGER NOT NULL DEFAULT 0;
