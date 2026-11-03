CREATE TABLE peer_trust (
    id INTEGER PRIMARY KEY CHECK(id=1),
    target TEXT NOT NULL,
    status TEXT NOT NULL
);
