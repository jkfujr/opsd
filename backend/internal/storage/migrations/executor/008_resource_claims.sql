ALTER TABLE operations ADD COLUMN resource_claims TEXT NOT NULL DEFAULT '[]';
ALTER TABLE operations ADD COLUMN start_error TEXT NOT NULL DEFAULT '';
