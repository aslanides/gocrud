package sqlstore

import (
	"database/sql"
	"fmt"

	"github.com/aslanides/gocrud/store"
	"github.com/aslanides/gocrud/x"
)

var log = x.Log("sqlstore")

type Sql struct {
	db *sql.DB
}

var sqlInsert *sql.Stmt
var sqlIsNew, sqlSelect string

func (s *Sql) Init(args ...string) {
	if len(args) != 3 {
		log.WithField("args", args).Fatal("Invalid arguments")
		return
	}

	dbtype := args[0]
	source := args[1]
	tablename := args[2]

	var err error
	s.db, err = sql.Open(dbtype, source)
	if err != nil {
		x.LogErr(log, err).Fatal("While opening connection")
		return
	}

	if err = s.db.Ping(); err != nil {
		x.LogErr(log, err).Fatal("While pinging db")
		return
	}

	var insert string
	switch dbtype {
	case "postgres":
		insert = fmt.Sprintf(`insert into %s (subject_id, subject_type, predicate,
	object, object_id, nano_ts, source) values ($1, $2, $3, $4, $5, $6, $7)`, tablename)
		sqlIsNew = fmt.Sprintf("select subject_id from %s where subject_id = $1 limit 1",
			tablename)
		sqlSelect = fmt.Sprintf(`select subject_id, subject_type, predicate,
	object, object_id, nano_ts, source from %s where subject_id = $1`, tablename)

	default:
		insert = fmt.Sprintf(`insert into %s (subject_id, subject_type, predicate,
	object, object_id, nano_ts, source) values (?, ?, ?, ?, ?, ?, ?)`, tablename)
		sqlIsNew = fmt.Sprintf("select subject_id from %s where subject_id = ? limit 1",
			tablename)
		sqlSelect = fmt.Sprintf(`select subject_id, subject_type, predicate,
	object, object_id, nano_ts, source from %s where subject_id = ?`, tablename)

	}

	sqlInsert, err = s.db.Prepare(insert)
	if err != nil {
		panic(err)
	}
}

func (s *Sql) IsNew(subject string) bool {
	rows, err := s.db.Query(sqlIsNew, subject)
	if err != nil {
		x.LogErr(log, err).Error("While checking is new")
		return false
	}
	defer rows.Close()
	var sub string
	isnew := true
	for rows.Next() {
		if err := rows.Scan(&sub); err != nil {
			x.LogErr(log, err).Error("While scanning")
			return false
		}
		log.WithField("subject_id", sub).Debug("Found existing subject_id")
		isnew = false
	}
	if err = rows.Err(); err != nil {
		x.LogErr(log, err).Error("While iterating")
		return false
	}
	return isnew
}

func (s *Sql) Commit(its []*x.Instruction) error {
	for _, it := range its {
		if _, err := sqlInsert.Exec(it.SubjectId, it.SubjectType, it.Predicate,
			it.Object, it.ObjectId, it.NanoTs, it.Source); err != nil {

			x.LogErr(log, err).Error("While inserting row in sql")
			return err
		}
	}
	return nil
}

func (s *Sql) GetEntity(subject string) (
	result []x.Instruction, rerr error) {

	rows, err := s.db.Query(sqlSelect, subject)
	if err != nil {
		x.LogErr(log, err).Error("While querying for entity")
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var i x.Instruction
		err := rows.Scan(&i.SubjectId, &i.SubjectType, &i.Predicate, &i.Object,
			&i.ObjectId, &i.NanoTs, &i.Source)
		if err != nil {
			x.LogErr(log, err).Error("While scanning")
			return result, err
		}
		result = append(result, i)
	}

	err = rows.Err()
	if err != nil {
		x.LogErr(log, err).Error("While finishing up on rows")
		return result, err
	}
	return result, nil
}

func (s *Sql) Iterate(fromId string, num int, ch chan x.Entity) (found int, last x.Entity, err error) {
	log.Fatal("Not implemented")
	return
}

func init() {
	log.Info("Initing sqlstore")
	store.Register("sqlstore", new(Sql))
}
