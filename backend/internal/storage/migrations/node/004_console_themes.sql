CREATE TABLE console_themes (
    installation_id TEXT PRIMARY KEY,
    theme_id TEXT NOT NULL,
    version TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    files_json TEXT NOT NULL,
    source TEXT NOT NULL,
    installed_at INTEGER NOT NULL,
    UNIQUE(theme_id, version)
);

CREATE TABLE console_theme_selection (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    installation_id TEXT REFERENCES console_themes(installation_id)
);
INSERT INTO console_theme_selection(id, installation_id) VALUES(1, NULL);
