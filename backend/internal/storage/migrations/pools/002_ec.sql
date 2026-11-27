ALTER TABLE storage_pools ADD COLUMN ec_operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE storage_pools ADD COLUMN ec_restore INTEGER NOT NULL DEFAULT 0 CHECK (ec_restore IN (0,1));
ALTER TABLE storage_pools ADD COLUMN ec_restore_error TEXT NOT NULL DEFAULT '';

CREATE TABLE storage_pool_ec_conversions (
  pool_id TEXT NOT NULL,
  volume_id INTEGER NOT NULL,
  generation INTEGER NOT NULL CHECK (generation > 0),
  operation_id TEXT NOT NULL UNIQUE,
  direction TEXT NOT NULL CHECK (direction IN ('encode','restore')),
  state TEXT NOT NULL CHECK (state IN ('pending','encoding','verifying','done','failed')),
  source_json TEXT NOT NULL CHECK (json_valid(source_json)),
  target_json TEXT NOT NULL CHECK (json_valid(target_json)),
  cutover_committed INTEGER NOT NULL DEFAULT 0 CHECK (cutover_committed IN (0,1)),
  cleanup_pending INTEGER NOT NULL DEFAULT 0 CHECK (cleanup_pending IN (0,1)),
  error_code TEXT,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (pool_id,volume_id,generation),
  FOREIGN KEY (pool_id,volume_id) REFERENCES storage_pool_volumes(pool_id,volume_id)
) STRICT;

CREATE UNIQUE INDEX one_active_volume_conversion
ON storage_pool_ec_conversions(pool_id,volume_id)
WHERE state IN ('pending','encoding','verifying') OR cleanup_pending = 1;
