CREATE TABLE state_cleanup(name TEXT PRIMARY KEY,sequence INTEGER NOT NULL DEFAULT 0,fingerprint TEXT NOT NULL DEFAULT '',anchor_id TEXT NOT NULL DEFAULT '',previous_id TEXT NOT NULL DEFAULT '');
INSERT INTO state_cleanup(name) VALUES('firewall');
