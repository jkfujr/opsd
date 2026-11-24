-- resource_key 取代 create_key：键改用 Galera 集群身份 + 库名，并且建库与删库共用。
--
-- 011 已经发布过（cf661ca），不能就地改写，否则已应用过它的节点会跳过迁移、
-- 之后启动时找不到新列。这里只做增量：加列、把旧键搬过来、换索引。
ALTER TABLE queue ADD COLUMN resource_key TEXT NOT NULL DEFAULT '';

UPDATE queue SET resource_key = create_key WHERE resource_key = '' AND create_key <> '';

DROP INDEX IF EXISTS queue_create_key;

CREATE UNIQUE INDEX IF NOT EXISTS queue_resource_key
ON queue(resource_key)
WHERE resource_key <> '' AND terminal = 0;
