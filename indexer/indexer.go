package indexer

import (
	"sort"
	"sync"

	"github.com/aslanides/gocrud/req"
	"github.com/aslanides/gocrud/search"
	"github.com/aslanides/gocrud/x"
)

var log = x.Log("indexer")

// Indexer functions are called automatically by store operations.
// These functions are used to determine which entities need updating,
// and then re-generate their corresponding documents, which then get
// re-indexed into search engine, overwriting past
// (using versioning, if available) documents.
type Indexer interface {
	// OnUpdate is called when an entity is updated due to a Commit
	// on either itself, or it's direct children. Note that each
	// child entity would also be called with OnUpdate. This function
	// should return the Entity Ids, which need regeneration.
	OnUpdate(x.Entity) []x.Entity

	// Regenerate would be called on entities which need to be reprocessed
	// due to a change. The workflow is:
	// store.Commit -> search.OnUpdate -> Regenerate
	Regenerate(x.Entity) x.Doc
}

var (
	mutex    sync.RWMutex
	indexers = make(map[string]Indexer)

	// Block if we have more than 1000 pending entities for update.
	updates = make(chan x.Entity, 1000)
	wg      = new(sync.WaitGroup)
)

func processUpdates(c *req.Context) {
	defer wg.Done()

	for entity := range c.Updates {
		idxr, pok := Get(entity.Kind)
		if !pok {
			continue
		}
		dirty := idxr.OnUpdate(entity)
		for _, de := range dirty {
			didxr, dok := Get(de.Kind)
			if !dok {
				continue
			}
			doc := didxr.Regenerate(de)
			log.WithField("doc", doc).Debug("Regenerated doc")
			if search.Get() == nil {
				continue
			}
			err := search.Get().Update(doc)
			if err != nil {
				x.LogErr(log, err).WithField("doc", doc).
					Error("While updating in search engine")
			}
		}
	}
	log.Info("Finished processing channel")
}

func Run(c *req.Context, numRoutines int) {
	if numRoutines <= 0 {
		log.WithField("num_routines", numRoutines).
			Fatal("Invalid number of goroutines for Indexer.")
		return
	}

	for i := 0; i < numRoutines; i++ {
		wg.Add(1)
		go processUpdates(c)
	}
}

func WaitForDone(c *req.Context) {
	log.Debug("Waiting for indexer to finish.")
	close(c.Updates)
	wg.Wait()
}

func Register(kind string, driver Indexer) {
	mutex.Lock()
	defer mutex.Unlock()
	if driver == nil {
		log.WithField("kind", kind).Fatal("nil indexer")
		return
	}
	if _, dup := indexers[kind]; dup {
		log.WithField("kind", kind).Fatal(
			"Another driver is already handling the same entity kind")
		return
	}
	indexers[kind] = driver
}

func Get(kind string) (i Indexer, p bool) {
	mutex.RLock()
	defer mutex.RUnlock()

	if driver, present := indexers[kind]; present {
		return driver, true
	} else {
		return nil, false
	}
}

func Kinds() []string {
	mutex.RLock()
	defer mutex.RUnlock()

	var list []string
	for kind := range indexers {
		list = append(list, kind)
	}
	sort.Strings(list)
	return list
}

func Num() int {
	mutex.RLock()
	defer mutex.RUnlock()

	return len(indexers)
}
