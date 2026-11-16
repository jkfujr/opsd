CREATE TABLE storage_targets(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,body TEXT NOT NULL);
CREATE TABLE object_storage_credentials(id TEXT PRIMARY KEY,target_id TEXT NOT NULL,secret TEXT NOT NULL);
CREATE TABLE monitoring_config(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL,body TEXT NOT NULL);
CREATE TABLE storage_checks(target_id TEXT PRIMARY KEY,object_key TEXT NOT NULL,body TEXT NOT NULL);
CREATE TABLE monitoring_workflows(id TEXT PRIMARY KEY,body TEXT NOT NULL);
