CREATE TABLE proxy_routing_state (
    cluster_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    body TEXT NOT NULL,
    PRIMARY KEY(cluster_id, instance_id)
);
