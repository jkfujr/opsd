CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE storage_pools (
  pool_id TEXT PRIMARY KEY,
  owner_node_id TEXT NOT NULL,
  name TEXT NOT NULL,
  normalized_name TEXT NOT NULL,
  revision INTEGER NOT NULL CHECK (revision >= 1),
  state TEXT NOT NULL CHECK (state IN (
    'creating','deploying','ready','degraded','read_only','unavailable','attention_required'
  )),
  ec_enabled INTEGER NOT NULL DEFAULT 0 CHECK (ec_enabled IN (0,1)),
  port_offset INTEGER NOT NULL DEFAULT 0 CHECK (port_offset BETWEEN 0 AND 16000),
  replication TEXT NOT NULL DEFAULT '010' CHECK (replication = '010'),
  operation_id TEXT NOT NULL UNIQUE,
  spec_hash TEXT NOT NULL,
  ca_pem TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE (owner_node_id, normalized_name)
) STRICT;

CREATE TABLE storage_pool_nodes (
  pool_id TEXT NOT NULL REFERENCES storage_pools(pool_id),
  node_id TEXT NOT NULL,
  failure_domain TEXT NOT NULL,
  private_ip TEXT NOT NULL,
  data_root_id TEXT NOT NULL,
  data_fs_uuid TEXT NOT NULL,
  control_fs_uuid TEXT NOT NULL,
  capacity_bytes INTEGER NOT NULL,
  control_bytes INTEGER NOT NULL CHECK (control_bytes > 0),
  roles_json TEXT NOT NULL CHECK (json_valid(roles_json)),
  ports_json TEXT NOT NULL CHECK (json_valid(ports_json)),
  parity_preferred INTEGER NOT NULL DEFAULT 0 CHECK (parity_preferred IN (0,1)),
  applied_revision INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK (state IN (
    'pending','deploying','ready','degraded','unavailable','attention_required'
  )),
  checked_at INTEGER,
  last_error TEXT,
  CHECK (capacity_bytes - control_bytes >= 2147483648),
  PRIMARY KEY (pool_id,node_id)
) STRICT;

CREATE TABLE storage_pool_volumes (
  pool_id TEXT NOT NULL REFERENCES storage_pools(pool_id),
  volume_id INTEGER NOT NULL CHECK (volume_id BETWEEN 0 AND 4294967295),
  collection TEXT NOT NULL,
  layout TEXT NOT NULL CHECK (layout IN ('replicated','ec')),
  state TEXT NOT NULL CHECK (state IN (
    'writable','sealed','converting','ready','degraded','unavailable'
  )),
  generation INTEGER NOT NULL DEFAULT 0 CHECK (generation >= 0),
  data_bytes INTEGER NOT NULL CHECK (data_bytes >= 0),
  max_bytes INTEGER NOT NULL CHECK (max_bytes > 0),
  last_write_at INTEGER,
  locations_json TEXT NOT NULL CHECK (json_valid(locations_json)),
  checked_at INTEGER NOT NULL,
  PRIMARY KEY (pool_id,volume_id)
) STRICT;
