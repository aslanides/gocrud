package leveldb

import (
	"errors"
	"fmt"

	"github.com/aslanides/gocrud/store"
	"github.com/aslanides/gocrud/x"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

var log = x.Log("leveldb")

type Leveldb struct {
	db  *leveldb.DB
	opt *opt.Options
}

func (l *Leveldb) SetBloomFilter(bits int) {
	l.opt = &opt.Options{
		Filter: filter.NewBloomFilter(bits),
	}
}

func (l *Leveldb) Init(args ...string) {
	if len(args) != 1 {
		log.WithField("args", args).Fatal("Invalid arguments")
		return
	}
	filepath := args[0]

	var err error
	l.db, err = leveldb.OpenFile(filepath, l.opt)
	if err != nil {
		x.LogErr(log, err).Fatal("While opening leveldb")
		return
	}
}

func (l *Leveldb) IsNew(id string) bool {
	slice := util.BytesPrefix([]byte(id))
	iter := l.db.NewIterator(slice, nil)
	isnew := true
	lg := log.WithField("id", id)
	for iter.Next() {
		if iter.Key != nil {
			isnew = false
			lg.WithField("key", string(iter.Key())).Debug("Found key")
		} else {
			lg.Debug("Found nil key")
		}
	}
	iter.Release()
	if err := iter.Error(); err != nil {
		x.LogErr(lg, err).Error("While iterating")
		return false
	}
	return isnew
}

func (l *Leveldb) Commit(its []*x.Instruction) error {
	var keys []string
	for _, it := range its {
		var key string
		for m := 0; m < 10; m++ {
			key = fmt.Sprintf("%s_%s", it.SubjectId, x.UniqueString(5))
			log.WithField("key", key).Debug("Checking existence of key")
			if has, err := l.db.Has([]byte(key), nil); err != nil {
				x.LogErr(log, err).WithField("key", key).Error("While check if key exists")
				continue
			} else if has {
				continue
			} else {
				break
			}
			log.Errorf("Exhausted %d tries", m)
			return errors.New("Exhausted tries")
		}
		log.WithField("key", key).Debug("Is unique")
		keys = append(keys, key)
	}

	b := new(leveldb.Batch)
	for idx, it := range its {
		key := []byte(keys[idx])
		buf, err := it.GobEncode()
		if err != nil {
			x.LogErr(log, err).Error("While encoding")
			return err
		}
		b.Put(key, buf)
	}
	if err := l.db.Write(b, nil); err != nil {
		x.LogErr(log, err).Error("While writing to db")
		return err
	}
	log.Debugf("%d instructions committed", len(its))

	return nil
}

func (l *Leveldb) GetEntity(id string) (result []x.Instruction, rerr error) {
	slice := util.BytesPrefix([]byte(id))
	iter := l.db.NewIterator(slice, nil)
	for iter.Next() {
		buf := iter.Value()
		if buf == nil {
			break
		}
		var i x.Instruction
		if err := i.GobDecode(buf); err != nil {
			x.LogErr(log, err).Error("While decoding")
			return result, err
		}
		result = append(result, i)
	}
	iter.Release()
	err := iter.Error()
	if err != nil {
		x.LogErr(log, err).Error("While iterating")
	}
	return result, err
}

func (l *Leveldb) Iterate(fromId string, num int,
	ch chan x.Entity) (rnum int, rlast x.Entity, rerr error) {
	slice := util.Range{Start: []byte(fromId)}
	iter := l.db.NewIterator(&slice, nil)

	rnum = 0
	handled := make(map[x.Entity]bool)
	for iter.Next() {
		buf := iter.Value()
		if buf == nil {
			break
		}
		var i x.Instruction
		if err := i.GobDecode(buf); err != nil {
			x.LogErr(log, err).Error("While decoding")
			return rnum, rlast, err
		}
		e := x.Entity{Kind: i.SubjectType, Id: i.SubjectId}
		rlast = e
		if _, present := handled[e]; present {
			continue
		}
		ch <- e
		handled[e] = true
		rnum += 1
		if rnum >= num {
			break
		}
	}
	iter.Release()
	err := iter.Error()
	if err != nil {
		x.LogErr(log, err).Error("While iterating")
	}
	return rnum, rlast, err
}

func init() {
	log.Info("Initing leveldb")
	l := new(Leveldb)
	l.SetBloomFilter(13)
	store.Register("leveldb", l)
}
