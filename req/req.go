// This package is the initialization point for the api.
// In particular, in your init (/main) function, the flow
// is to create a req.Context and fill in required options.
// Setting table prefix, length for unique strings generated
// to assign to new entities, and setting the storage system.
package req

import "github.com/aslanides/gocrud/x"

var log = x.Log("req")

type Context struct {
	NumCharsUnique int // 62^num unique strings
	Updates        chan x.Entity
	HasIndexer     bool
}

func NewContext(numChars int) *Context {
	ctx := new(Context)
	ctx.NumCharsUnique = numChars
	ctx.HasIndexer = false
	ctx.Updates = nil
	return ctx
}

func NewContextWithUpdates(numChars, buffer int) *Context {
	ctx := new(Context)
	ctx.NumCharsUnique = numChars
	ctx.Updates = make(chan x.Entity, buffer)
	ctx.HasIndexer = true
	return ctx
}
