DROP INDEX one_local_operation;
CREATE UNIQUE INDEX one_local_operation ON operations(terminal)
 WHERE terminal=0 AND COALESCE(json_extract(action,'$.kind'),'')<>'shell';
CREATE UNIQUE INDEX one_task_operation ON operations(json_extract(action,'$.shell.task_id'))
 WHERE terminal=0 AND json_extract(action,'$.kind')='shell';
