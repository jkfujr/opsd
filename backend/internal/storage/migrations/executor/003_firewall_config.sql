CREATE TABLE firewall_confirmed (
    id INTEGER PRIMARY KEY CHECK(id=1),
    body TEXT NOT NULL
);
