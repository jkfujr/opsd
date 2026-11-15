CREATE TABLE shared_local(id INTEGER PRIMARY KEY CHECK(id=1),secret TEXT NOT NULL);
CREATE TABLE shared_flow_exports(id TEXT NOT NULL,revision INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(id,revision));
