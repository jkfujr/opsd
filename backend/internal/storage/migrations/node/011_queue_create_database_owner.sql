-- 同一集群同一库名，最多只允许一个**在途**建库操作。
--
-- 回执键按已认证来源隔离后，换入口重发同一个建库请求不再命中回执，
-- 资源占用因此必须由资源本身保证。载荷是加密存放的，不能直接在其上建索引，
-- 所以受理时把“Galera 集群身份 + 库名”写成一列明文资源键，再对这一列做部分
-- 唯一索引：并发或跨入口的第二次受理在写入时就失败，而不是靠应用侧“先查后写”。
--
-- 建库与删库共用同一个键：删除在途时也占用资源，删除成功后才释放。
ALTER TABLE queue ADD COLUMN resource_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS queue_resource_key
ON queue(resource_key)
WHERE resource_key <> '' AND terminal = 0;
