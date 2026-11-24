-- 同一集群同一库名，最多只允许一个**在途**建库操作。
--
-- 回执键按已认证来源隔离后，换入口重发同一个建库请求不再命中回执，
-- 资源占用因此必须由资源本身保证。载荷是加密存放的，不能直接在其上建索引，
-- 所以受理时把“集群 + 库名”写成一列明文资源键，再对这一列做部分唯一索引：
-- 并发或跨入口的第二次受理在写入时就失败，而不是靠应用侧的“先查后写”。
ALTER TABLE queue ADD COLUMN create_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS queue_create_key
ON queue(create_key)
WHERE create_key <> '' AND terminal = 0;
