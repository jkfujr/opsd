-- 幂等分桶改用 (已认证来源节点, 声明操作者, 请求键) 三元组。
--
-- 声明操作者可以区分同一来源上的不同请求，但不能提供人员级隔离：
-- 授权始终以已认证来源为准。旧记录的 actor 为空，仍按 (origin, '') 唯一。
ALTER TABLE queue ADD COLUMN actor TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS queue_key;

CREATE UNIQUE INDEX IF NOT EXISTS queue_key ON queue(origin,actor,request_key) WHERE request_key<>'';
