// Package main to demonstrate usage of the gocrud apis,
// using a social network as an example.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/aslanides/gocrud/indexer"
	"github.com/aslanides/gocrud/req"
	"github.com/aslanides/gocrud/search"
	"github.com/aslanides/gocrud/store"
	"github.com/aslanides/gocrud/x"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	// _ "github.com/aslanides/gocrud/drivers/elasticsearch"
	_ "github.com/aslanides/gocrud/drivers/leveldb"
	_ "github.com/aslanides/gocrud/drivers/memsearch"
	// _ "github.com/aslanides/gocrud/drivers/datastore"
	// _ "github.com/aslanides/gocrud/drivers/sqlstore"
	// _ "github.com/aslanides/gocrud/drivers/cassandra"
	// _ "github.com/aslanides/gocrud/drivers/mongodb"
	// _ "github.com/aslanides/gocrud/drivers/rethinkdb"
)

var debug = flag.Bool("debug", false, "Set debug level.")

var log = x.Log("social")
var c *req.Context

type Like struct {
	Id string `json:"id,omitempty"`
}

type Comment struct {
	Id      string    `json:"id,omitempty"`
	Comment []Comment `json:"Comment,omitempty"`
	Like    []Like    `json:"Like,omitempty"`
}

type Post struct {
	Id      string    `json:"id,omitempty"`
	Comment []Comment `json:"Comment,omitempty"`
	Like    []Like    `json:"Like,omitempty"`
}

type User struct {
	Id   string `json:"id,omitempty"`
	Post []Post `json:"Post,omitempty"`
}

type SimpleIndexer struct {
}

func (si SimpleIndexer) OnUpdate(e x.Entity) (result []x.Entity) {
	// Also update the parent entity.
	parentid, err := store.Parent(e.Id)
	if err == nil {
		r, rerr := store.NewQuery(parentid).Run()
		if rerr == nil {
			ep := x.Entity{Id: parentid, Kind: r.Kind}
			result = append(result, ep)
		}
	}

	result = append(result, e)
	return
}

func (si SimpleIndexer) Regenerate(e x.Entity) (rdoc x.Doc) {
	rdoc.Id = e.Id
	rdoc.Kind = e.Kind
	rdoc.NanoTs = time.Now().UnixNano()

	if e.Kind == "Post" {
		// If Post, figure out the total activity on it, so we can sort by that.
		result, err := store.NewQuery(e.Id).UptoDepth(1).Run()
		if err != nil {
			x.LogErr(log, err).Fatal("While querying db")
			return rdoc
		}
		data := result.ToMap()
		data["activity"] = len(result.Children)
		rdoc.Data = data

	} else {
		result, err := store.NewQuery(e.Id).UptoDepth(0).Run()
		if err != nil {
			x.LogErr(log, err).Fatal("While querying db")
			return rdoc
		}
		rdoc.Data = result.ToMap()
	}

	return
}

func newUser() string {
	return "uid_" + x.UniqueString(3)
}

const sep1 = "--------------------------------"
const sep2 = "================================"

func prettyPrintResult(result store.Result) {
	mresult := result.ToMap()
	js, err := json.MarshalIndent(mresult, "", "'  ")
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("\n%s\n%s\n%s\n\n", sep1, string(js), sep2)
}

func printAndGetUser(uid string) (user User) {
	result, err := store.NewQuery(uid).UptoDepth(10).Run()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	prettyPrintResult(*result)

	js, err := result.ToJson()
	if err := json.Unmarshal(js, &user); err != nil || len(user.Post) == 0 {
		log.Fatalf("Error: %v", err)
	}
	return user
}

func processChannel(ch chan x.Entity, done chan bool) {
	for entity := range ch {
		fmt.Println("Entity stored:", entity.Kind, entity.Id)
	}
	done <- true
}

func main() {
	rand.Seed(0) // Keep output consistent.
	flag.Parse()
	if *debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.ErrorLevel)
	}

	c = req.NewContextWithUpdates(10, 1000) // 62^10 permutations

	// Initialize leveldb.
	dirname, err := ioutil.TempDir("", "ldb_")
	if err != nil {
		log.Fatalf("While creating temp directory: %v\n", err)
		return
	}
	defer os.RemoveAll(dirname)
	store.Get().Init(dirname)

	// Initialize Elasticsearch.
	// search.Get().Init("http://192.168.59.103:9200")

	// Other possible initializations. Remember to import the right driver.
	// store.Get().Init("mysql", "root@tcp(127.0.0.1:3306)/test", "instructions")
	// store.Get().Init("cassone", "crudtest", "instructions")
	// store.Get().Init("192.168.59.103:27017", "crudtest", "instructions")
	// store.Get().Init("192.168.59.103:28015", "test", "instructions")

	search.Get().Init("memsearch")
	indexer.Register("Post", SimpleIndexer{})
	indexer.Register("Like", SimpleIndexer{})
	indexer.Register("Comment", SimpleIndexer{})
	indexer.Run(c, 2)
	defer indexer.WaitForDone(c)

	log.Debug("Store initialized. Checking search...")
	uid := newUser()

	// Let's get started. User 'uid' creates a new Post.
	// This Post shares a url, adds some text and some tags.
	tags := [3]string{"search", "cat", "videos"}
	err = store.NewUpdate("User", uid).SetSource(uid).AddChild("Post").
		Set("url", "www.google.com").Set("body", "You can search for cat videos here").
		Set("tags", tags).Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Now let's add a comment and two likes to our new post.
	// One user would add a comment and one like. Another user would
	// just like the post.
	//
	// It's best to have the same 'source' for one set of operations.
	// In REST APIs, this is how things would always be. Each REST call
	// is from one user (and never two different users).
	// This way the creation of like "entity", and the properties
	// of that new like entity have the same source.
	//
	// So, here's Step 1: A new user would add a comment, and like the post.
	fmt.Print("Added a new post by user")
	user := printAndGetUser(uid)
	post := user.Post[0]

	p := store.NewUpdate("Post", post.Id).SetSource(newUser())
	p.AddChild("Like").Set("thumb", 1)
	p.AddChild("Comment").Set("body",
		fmt.Sprintf("Comment %s on the post", x.UniqueString(2)))
	err = p.Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Step 2: Another user would now like the post.
	p = store.NewUpdate("Post", post.Id).SetSource(newUser())
	p.AddChild("Like").Set("thumb", 1)
	err = p.Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Print("Added a Comment and 2 Likes on Post")

	user = printAndGetUser(uid)
	post = user.Post[0]
	if len(post.Comment) == 0 {
		log.Fatalf("No comment found: %+v", post)
	}
	comment := post.Comment[0]

	// Now another user likes and replies to the comment that was added above.
	// So, it's a comment within a comment.
	p = store.NewUpdate("Comment", comment.Id).SetSource(newUser())
	p.AddChild("Like").Set("thumb", 1)
	p.AddChild("Comment").Set("body",
		fmt.Sprintf("Comment %s on comment", x.UniqueString(2)))
	err = p.Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Print("Added a Comment and a Like on Comment")
	user = printAndGetUser(uid)
	post = user.Post[0]
	if len(post.Comment) == 0 {
		log.Fatalf("No comment found: %+v", post)
	}
	comment = post.Comment[0]
	if len(comment.Like) == 0 {
		log.Fatalf("No like found: %+v", comment)
	}
	like := comment.Like[0]

	// So far we have this structure:
	// User
	//  L Post
	//         L 2 * Like
	//         L Comment
	//            L Comment
	//            L Like

	// This is what most social platforms do. But, let's go
	// one level further, and also comment on the Likes on Comment.
	// User
	//    L Post
	//         L 2 * Like
	//         L Comment
	//            L Comment
	//            L Like
	//                 L Comment

	// Another user Comments on the Like on Comment on Post.

	p = store.NewUpdate("Like", like.Id).SetSource(newUser()).
		AddChild("Comment").Set("body",
		fmt.Sprintf("Comment %s on Like", x.UniqueString(2)))
	err = p.Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Print("Added Comment on Like")
	user = printAndGetUser(uid)

	{
		docs, err := search.Get().NewQuery("Like").Order("data.source").Run()
		if err != nil {
			x.LogErr(log, err).Fatal("While searching for Post")
			return
		}
		for _, doc := range docs {
			log.WithField("doc", doc).Debug("Resulting doc")
		}
		log.Debug("Search query over")
	}

	post = user.Post[0]
	if len(post.Comment) == 0 {
		log.Fatalf("No comment found: %+v", post)
	}
	comment = post.Comment[0]
	p = store.NewUpdate("Comment", comment.Id).SetSource(newUser()).Set("censored", true)
	err = p.Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	q := store.NewQuery(comment.Id).UptoDepth(0)
	result, err := q.Run()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Print("Set censored=true on comment")
	prettyPrintResult(*result)

	user = printAndGetUser(uid)
	post = user.Post[0]
	if pid, err := store.Parent(post.Id); err == nil {
		if pid != user.Id {
			log.Fatal("Post's parent id doesn't match user id.")
			return
		}
		log.WithFields(logrus.Fields{
			"id":        post.Id,
			"parent_id": pid,
			"user_id":   user.Id,
		}).Debug("Parent id matches")
	} else {
		log.Fatal(err.Error())
		return
	}

	if len(post.Like) == 0 {
		log.Fatalf("No like found: %+v", post)
	}
	like = post.Like[0]
	p = store.NewUpdate("Like", like.Id).SetSource(newUser()).MarkDeleted()
	err = p.Execute(c)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	q = store.NewQuery(uid).Collect("Post")
	q.Collect("Like").UptoDepth(10)
	q.Collect("Comment").UptoDepth(10).FilterOut("censored")
	result, err = q.Run()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	fmt.Print("Filter out censored Comment and mark one Like as deleted.")
	prettyPrintResult(*result)
	// By now we have a fairly complex Post structure. CRUD for
	// which would have been a lot of work to put together using
	// typical SQL / NoSQL tables.

	{
		ch := make(chan x.Entity, 10)
		done := make(chan bool)
		go processChannel(ch, done)
		num, last, err := store.Get().Iterate("", 100, ch)
		if err != nil {
			x.LogErr(log, err).Fatal("While iterating")
			return
		}
		fmt.Printf("Found %d results\n", num)
		fmt.Printf("Last Entity: %+v\n", last)
		close(ch)
		<-done
	}

	{
		fmt.Println()
		fmt.Println()
		fmt.Print("Searching for doc with url = www.google.com")
		q := search.Get().NewQuery("Post").Order("-data.activity")
		q.NewAndFilter().AddExact("data.url", "www.google.com")
		docs, err := q.Run()
		if err != nil {
			x.LogErr(log, err).Fatal("While searching for Post")
			return
		}
		for _, doc := range docs {
			js, err := json.MarshalIndent(doc, "", "'  ")
			if err != nil {
				log.Fatalf("While marshal: %v\n", err)
				return
			}
			fmt.Printf("\n%s\n%s\n%s\n\n", sep1, string(js), sep2)
			// log.WithField("doc", doc).Debug("Resulting doc")
		}
		log.Debug("Search query over")
	}
}
