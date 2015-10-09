CREATE TABLE tasks (
    task_id    integer   PRIMARY KEY AUTOINCREMENT,
    user       text      NOT NULL,
    created    timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
    name       text      ,
    size       real      NOT NULL,
    plan       real      NOT NULL,
    mode       integer   NOT NULL,
    total_done real      NOT NULL DEFAULT 0,
    last_done  real      ,
    last_time  timestamp 
);

CREATE TABLE task_history (
    task_id integer   NOT NULL REFERENCES tasks( task_id ),
    time    timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
    done    real      NOT NULL
);

CREATE TRIGGER tasks_update AFTER INSERT ON task_history FOR EACH ROW BEGIN 
    UPDATE tasks SET total_done = total_done + NEW.done, last_done = NEW.done, 
		last_time = NEW.time WHERE task_id = NEW.task_id;
END;
