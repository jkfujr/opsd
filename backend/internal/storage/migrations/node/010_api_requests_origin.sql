-- 回执同样按已认证来源归属：声明操作者只区分来源内的不同请求。
ALTER TABLE api_requests ADD COLUMN origin TEXT NOT NULL DEFAULT '';

DROP INDEX IF EXISTS api_requests_actor_key;

CREATE UNIQUE INDEX IF NOT EXISTS api_requests_owner ON api_requests(origin,actor,request_key);
