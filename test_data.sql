INSERT INTO tasks( user, created, name, size, plan, mode ) VALUES
    ("test", "2014-01-05", "Task 1", 10, 1, 1),
    ("test", "2014-05-11", "Task 2", 10, 2, 2),
    ("test", "2014-03-01", "Task 3", 20, 1, 3);

INSERT INTO task_history( task_id, time, done ) VALUES
    (1, "2014-01-06",  1),
    (1, "2014-01-07",  1),
    (1, "2014-01-08", -1),
    (1, "now",         1), -- little hack here, it will always set to today
    (2, "2014-05-11",  2),
    (2, "2014-05-12",  1),
    (3, "2014-03-01",  1);
