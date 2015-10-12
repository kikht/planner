package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	_ "github.com/mattn/go-sqlite3"
	"gitlab.ict.sbras.ru/kikht/gohttpserv"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

//go:generate rm -f planner.db
//go:generate sqlite3 planner.db ".read schema.sql"
//go:generate stringer -type WorkMode

type WorkMode int

const (
	Everyday WorkMode = iota + 1
	Workdays
	Holidays

	WorkModeNum
	WorkModeFirst = Everyday
)

func (mode WorkMode) MarshalJSON() ([]byte, error) {
	return []byte("\"" + mode.String() + "\""), nil
}

func (mode *WorkMode) UnmarshalJSON(data []byte) error {
	str := string(data[1 : len(data)-1])
	switch str {
	case Everyday.String():
		*mode = Everyday
		return nil
	case Workdays.String():
		*mode = Workdays
		return nil
	case Holidays.String():
		*mode = Holidays
		return nil
	}
	return errors.New("Unknown WorkMode: " + str)
}

func isHoliday(day time.Time) bool {
	//TODO: official holidays and their movements
	weekday := day.Weekday()
	return weekday == time.Sunday || weekday == time.Saturday
}

func dayMatchesMode(day time.Time, mode WorkMode) bool {
	switch mode {
	case Everyday:
		return true
	case Workdays:
		return !isHoliday(day)
	case Holidays:
		return isHoliday(day)
	}
	panic("Unknown WorkMode " + mode.String())
}

func todayModes() []WorkMode {
	res := make([]WorkMode, 0, WorkModeNum)
	today := time.Now()
	for mode := WorkModeFirst; mode < WorkModeNum; mode++ {
		if dayMatchesMode(today, mode) {
			res = append(res, mode)
		}
	}
	return res
}

type Task struct {
	Id      int64 `json:"id"`
	Name    string
	Created string
	End     string
	Size    float64
	Done    float64
	Plan    float64
	Mode    WorkMode
	Today   bool
	NewItem bool `json:",omitempty"`
}

const DateFormat = "2006-01-02"

func (task Task) getPlannedEnd() time.Time {
	curDone := task.Done
	curDay := time.Now()
	if !task.Today {
		curDone += task.Plan
	}
	for curDone < task.Size {
		curDay = curDay.AddDate(0, 0, 1)
		if dayMatchesMode(curDay, task.Mode) {
			curDone += task.Plan
		}
	}
	return curDay
}

func taskList(w http.ResponseWriter, req *http.Request) {
	user := currentUser(w, req)
	if user == "" {
		return
	}

	modes := todayModes()
	var modeClause bytes.Buffer
	for i, mode := range modes {
		if i != 0 {
			modeClause.WriteString(" OR ")
		}
		modeClause.WriteString("t.mode = ")
		modeClause.WriteString(strconv.Itoa(int(mode)))
	}

	query := `
SELECT task_id, name, created, size, total_done, plan, mode, 
       (date(last_time) = date("now") and last_done > 0) today
FROM tasks t
WHERE t.user = ?
  AND (` + modeClause.String() + `)
  AND (total_done < size OR today > 0)
ORDER BY name`

	rows, err := database.Query(query, user)
	if err != nil {
		http.Error(w, "Error while querying database",
			http.StatusInternalServerError)
		log.Print(err)
		return
	}
	defer rows.Close()
	var result []Task
	for rows.Next() {
		var task Task
		var created time.Time
		if err := rows.Scan(&task.Id, &task.Name, &created, &task.Size,
			&task.Done, &task.Plan, &task.Mode, &task.Today); err != nil {

			http.Error(w, "Error while querying database",
				http.StatusInternalServerError)
			log.Print(err)
			return
		}

		task.Created = created.Format(DateFormat)
		task.End = task.getPlannedEnd().Format(DateFormat)
		result = append(result, task)
	}

	err = json.NewEncoder(w).Encode(result)
	if err != nil {
		log.Print(err)
	}
}

func saveTask(w http.ResponseWriter, req *http.Request) {
	user := currentUser(w, req)
	if user == "" {
		return
	}

	var task Task
	err := json.NewDecoder(req.Body).Decode(&task)
	if err != nil {
		http.Error(w, "Error while parsing request body", http.StatusBadRequest)
		log.Print(err)
		return
	}

	//TODO: verify task data
	if task.Mode < WorkModeFirst || task.Mode >= WorkModeNum ||
		task.Plan <= 0 || task.Size <= 0 {
		http.Error(w, "Invalid task data", http.StatusBadRequest)
		return
	}

	tx, err := database.Begin()
	if err != nil {
		http.Error(w, "Error while queriyng database",
			http.StatusInternalServerError)
		log.Print(err)
		return
	}

	var done float64
	if task.NewItem {
		created := time.Now()
		res, err := tx.Exec(`INSERT INTO tasks( 
			user, created, name, size, plan, mode ) VALUES( ?, ?, ?, ?, ?, ? )`,
			user, created, task.Name, task.Size, task.Plan, task.Mode)
		if err != nil {
			http.Error(w, "Error while queriyng database",
				http.StatusInternalServerError)
			log.Print(err)
			tx.Rollback()
			return
		}
		task.Id, err = res.LastInsertId()
		if err != nil {
			http.Error(w, "Error while queriyng database",
				http.StatusInternalServerError)
			log.Print(err)
			tx.Rollback()
			return
		}
		task.NewItem = false
		task.Created = created.Format(DateFormat)
		done = task.Done
	} else {
		var oldDone float64
		err := tx.QueryRow(
			"SELECT total_done FROM tasks WHERE task_id = ? AND user = ?",
			task.Id, user).Scan(&oldDone)
		if err != nil {
			http.Error(w, "Task not found", http.StatusNotFound)
			if err != sql.ErrNoRows {
				log.Print(err)
			}
			tx.Rollback()
			return
		}
		_, err = tx.Exec(`UPDATE tasks SET name = ?, size = ?, plan = ?, 
			mode = ? WHERE task_id = ?`,
			task.Name, task.Size, task.Plan, task.Mode, task.Id)
		if err != nil {
			http.Error(w, "Error while queriyng database",
				http.StatusInternalServerError)
			log.Print(err)
			tx.Rollback()
			return
		}
		done = task.Done - oldDone
	}

	if done != 0 {
		_, err = tx.Exec(
			"INSERT INTO task_history( task_id, done ) VALUES( ?, ? )",
			task.Id, done)
		if err != nil {
			http.Error(w, "Error while queriyng database",
				http.StatusInternalServerError)
			log.Print(err)
			tx.Rollback()
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Error while queriyng database",
			http.StatusInternalServerError)
		log.Print(err)
		return
	}

	task.Today = done > 0
	task.End = task.getPlannedEnd().Format(DateFormat)
	err = json.NewEncoder(w).Encode(task)
	if err != nil {
		log.Print(err)
	}
}

func todayChange(w http.ResponseWriter, req *http.Request) {
	user := currentUser(w, req)
	if user == "" {
		return
	}

	var task Task
	err := json.NewDecoder(req.Body).Decode(&task)
	if err != nil {
		http.Error(w, "Error while parsing request body", http.StatusBadRequest)
		log.Print(err)
		return
	}

	tx, err := database.Begin()
	if err != nil {
		http.Error(w, "Error while queriyng database",
			http.StatusInternalServerError)
		log.Print(err)
		return
	}

	query := `
SELECT t.plan, t.total_done, TOTAL(h.done) delta,
       CASE WHEN date(t.last_time) = date("now") THEN t.last_done ELSE 0 END
FROM tasks t
LEFT OUTER JOIN task_history h 
	ON t.task_id = h.task_id AND date(h.time) = date("now")
WHERE t.task_id = ? AND t.user = ?`
	var delta, lastDone float64
	err = tx.QueryRow(query, task.Id, user).Scan(
		&task.Plan, &task.Done, &delta, &lastDone)
	if err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		if err != sql.ErrNoRows {
			log.Print(err)
		}
		tx.Rollback()
		return
	}

	log.Printf("state: today %v, lastDone: %v", task.Today, lastDone)
	if task.Today != (lastDone <= 0) {
		http.Error(w, "Invalid task state", http.StatusConflict)
		tx.Rollback()
		return
	}

	var done float64
	if task.Today {
		done = task.Plan
	} else {
		if lastDone < task.Plan && delta < task.Plan {
			done = -lastDone
		} else {
			done = -task.Plan
		}
		if done < -task.Done {
			done = -task.Done
		}
	}

	_, err = tx.Exec("INSERT INTO task_history( task_id, done ) VALUES( ?, ? )",
		task.Id, done)
	if err != nil {
		http.Error(w, "Error while queriyng database",
			http.StatusInternalServerError)
		log.Print(err)
		tx.Rollback()
		return
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Error while queriyng database",
			http.StatusInternalServerError)
		log.Print(err)
		return
	}

	task.Done += done
	task.End = task.getPlannedEnd().Format(DateFormat)
	err = json.NewEncoder(w).Encode(task)
	if err != nil {
		log.Print(err)
	}
}

func currentUser(w http.ResponseWriter, req *http.Request) string {
	//TODO: actual implementation that redirects unauthenticated users
	return "test"
}

var database *sql.DB

func main() {
	var err error
	database, err = sql.Open("sqlite3", "planner.db")
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	http.HandleFunc("/task-list", taskList)
	http.HandleFunc("/save-task", saveTask)
	http.HandleFunc("/today-change", todayChange)
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/" {
			http.ServeFile(w, req, "index.html")
		} else {
			http.NotFound(w, req)
		}
	})

	gohttpserv.Serve(nil)	
}

//TODO: move server initialization code to separate package
//and use in hvault-wms
func startServer() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	err = http.Serve(listener, nil)
	log.Fatal(err)
}
