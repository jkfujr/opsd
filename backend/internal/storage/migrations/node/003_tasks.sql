CREATE TABLE scheduled_tasks(id TEXT PRIMARY KEY,revision INTEGER NOT NULL,body TEXT NOT NULL);

UPDATE account_navigation SET order_json=(
 SELECT json_group_array(value) FROM (
  SELECT value,key*2 AS position FROM json_each(account_navigation.order_json)
  UNION ALL SELECT 'tools',key*2-1 FROM json_each(account_navigation.order_json) WHERE value='settings'
  ORDER BY position
 )
),revision=revision+1 WHERE NOT EXISTS(SELECT 1 FROM json_each(order_json) WHERE value='tools');

CREATE TABLE compose_projects(id TEXT PRIMARY KEY,body TEXT NOT NULL);
CREATE INDEX queue_task_resource ON queue(kind,json_extract(body,'$.resource_id'),seq DESC);
