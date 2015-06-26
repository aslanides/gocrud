package api

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/crud/req"
	"github.com/crud/x"
)

type Query struct {
	kind      string
	id        string
	filterOut map[string]bool
	maxDepth  int
	children  []*Query
	parent    *Query
}

type Object struct {
	Value  interface{}
	Source string
	NanoTs int64
}

type Result struct {
	Id       string
	Kind     string
	Children []*Result
	Columns  map[string]Object
}

type runResult struct {
	Result *Result
	Err    error
}

func NewQuery(kind, id string) *Query {
	q := new(Query)
	q.kind = kind
	q.id = id
	return q
}

func (q *Query) UptoDepth(level int) *Query {
	q.maxDepth = level
	return q
}

func (q *Query) Collect(kind string) *Query {
	for _, child := range q.children {
		if child.kind == kind {
			return child
		}
	}
	child := new(Query)
	child.parent = q
	child.kind = kind
	q.children = append(q.children, child)
	return child
}

func (q *Query) FilterOut(property string) *Query {
	if len(q.filterOut) == 0 {
		q.filterOut = make(map[string]bool)
	}
	q.filterOut[property] = true
	return q
}

func (q *Query) root() *Query {
	for q.parent != nil {
		q = q.parent
	}
	return q
}

func (q *Query) doRun(c *req.Context, level, max int, ch chan runResult) {
	log.Debugf("Query: %+v", q)
	its, err := c.Store.GetEntity(c.TablePrefix, q.id)
	if err != nil {
		x.LogErr(log, err).Error("While retrieving: ", q.id)
		ch <- runResult{Result: nil, Err: err}
		return
	}
	if len(its) == 0 {
		ch <- runResult{Result: new(Result), Err: nil}
		return
	}
	sort.Sort(x.Its(its))

	follow := make(map[string]*Query)
	for _, child := range q.children {
		follow[child.kind] = child
		log.WithField("kind", child.kind).WithField("child", child).Debug("Following")
	}

	result := new(Result)
	result.Columns = make(map[string]Object)
	it := its[0]
	result.Id = it.SubjectId
	result.Kind = it.SubjectType

	waitTimes := 0
	childChan := make(chan runResult)
	for _, it := range its {
		if _, fout := q.filterOut[it.Predicate]; fout {
			log.WithField("id", result.Id).
				WithField("predicate", it.Predicate).
				Debug("Discarding due to predicate filter")
			ch <- runResult{Result: new(Result), Err: nil}
			return
		}

		if len(it.ObjectId) == 0 {
			o := Object{NanoTs: it.NanoTs, Source: it.Source}
			if err := json.Unmarshal(it.Object, &o.Value); err != nil {
				x.LogErr(log, err).Error("While unmarshal")
				ch <- runResult{Result: nil, Err: err}
				return
			}

			result.Columns[it.Predicate] = o
			continue
		}

		if child, fw := follow[it.Predicate]; fw {
			child.id = it.ObjectId
			// Use child's maxDepth here, instead of parent's.
			waitTimes += 1
			go child.doRun(c, 0, child.maxDepth, childChan)
			/*
				if cr, err := child.doRun(c, 0, child.maxDepth); err == nil {
					if len(cr.Id) > 0 && len(cr.Kind) > 0 {
						result.Children = append(result.Children, cr)
					}
				} else {
					x.LogErr(log, err).Error("While doRun")
				}
			*/
			continue
		}

		if len(it.ObjectId) > 0 && level < max {
			child := new(Query)
			child.id = it.ObjectId

			waitTimes += 1
			go child.doRun(c, level+1, max, childChan)
			/*
				if cr, err := child.doRun(c, level+1, max); err == nil {
					result.Children = append(result.Children, cr)
				} else {
					x.LogErr(log, err).Error("While doRun")
				}
			*/
		}
	}
	for i := 0; i < waitTimes; i++ {
		log.Debugf("Waiting for children subroutines: %v", i)
		rr := <-childChan
		log.Debugf("Waiting done")
		if rr.Err != nil {
			x.LogErr(log, err).Error("While child doRun")
		} else {
			if len(rr.Result.Id) > 0 && len(rr.Result.Kind) > 0 {
				result.Children = append(result.Children, rr.Result)
			}
		}
	}

	ch <- runResult{Result: result, Err: nil}
	return
}

func (q *Query) Run(c *req.Context) (result *Result, rerr error) {
	q = q.root()

	ch := make(chan runResult)
	go q.doRun(c, 0, q.maxDepth, ch)
	rr := <-ch // Blocking wait
	return rr.Result, rr.Err
}

func (r *Result) Debug(level int) {
	log.Debugf("Result level %v: %+v", level, r)
	for _, child := range r.Children {
		child.Debug(level + 1)
	}
}

func (r *Result) toJson() (data map[string]interface{}) {
	data = make(map[string]interface{})
	data["id"] = r.Id
	data["kind"] = r.Kind
	var ts int64
	for k, o := range r.Columns {
		data[k] = o.Value
		data["source"] = o.Source // Loss of information.
		if o.NanoTs > ts {
			ts = o.NanoTs
		}
	}
	data["ts_millis"] = int(ts / 1000000) // Loss of information. Picking up latest mod time.
	kinds := make(map[string]bool)
	for _, child := range r.Children {
		kinds[child.Kind] = true
	}

	for kind := range kinds {
		var l []map[string]interface{}
		for _, child := range r.Children {
			if kind != child.Kind {
				continue
			}
			l = append(l, child.toJson())
		}
		data[kind] = l
	}

	return
}

func (r *Result) ToJson() ([]byte, error) {
	data := r.toJson()
	return json.Marshal(data)
}

func (r *Result) WriteJsonResponse(w http.ResponseWriter) {
	data, err := r.ToJson()
	if err != nil {
		x.SetStatus(w, x.E_ERROR, err.Error())
		return
	}
	_, err = w.Write(data)
	if err != nil {
		x.SetStatus(w, x.E_ERROR, err.Error())
		return
	}
}