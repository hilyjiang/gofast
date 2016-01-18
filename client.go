package gofast

import (
	"fmt"
	"io"
	"net"
)

// client is the default implementation of Client
type client struct {
	conn *conn

	chanID chan uint16
	ids    map[uint16]bool
}

// AllocID implements Client.AllocID
func (c *client) AllocID() (reqID uint16) {
	for {
		reqID = <-c.chanID
		if c.ids[reqID] != true {
			break
		}
	}
	c.ids[reqID] = true
	return
}

// ReleaseID implements Client.ReleaseID
func (c *client) ReleaseID(reqID uint16) {
	c.ids[reqID] = false
	go func() {
		// release the ID back to channel for reuse
		// use goroutine to prevent blocking ReleaseID
		c.chanID <- reqID
	}()
}

// Handle implements Client.Handle
func (c *client) Handle(wOut, wErr io.Writer, req *Request) (err error) {
	defer c.ReleaseID(req.GetID())

	err = c.conn.writeBeginRequest(req.GetID(), uint16(roleResponder), 0)
	if err != nil {
		return
	}
	err = c.conn.writePairs(typeParams, req.GetID(), req.Params)
	if err != nil {
		return
	}
	if len(req.Content) > 0 {
		err = c.conn.writeRecord(typeStdin, req.GetID(), req.Content)
		if err != nil {
			return
		}
	}

	var rec record

readLoop:
	for {
		if err := rec.read(c.conn.rwc); err != nil {
			break
		}

		// different output type for different stream
		switch rec.h.Type {
		case typeStdout:
			wOut.Write(rec.content())
		case typeStderr:
			wErr.Write(rec.content())
		case typeEndRequest:
			break readLoop
		default:
			panic(fmt.Sprintf("unexpected type %#v in readLoop", rec.h.Type))
		}
	}

	return
}

// NewRequest implements Client.NewRequest
func (c *client) NewRequest() *Request {
	return &Request{
		ID:     c.AllocID(),
		Params: make(map[string]string),
	}
}

// Client is a client interface of FastCGI
// application process through given
// connection (net.Conn)
type Client interface {

	// Handle takes care of a proper FastCGI request
	Handle(wOut, wErr io.Writer, req *Request) (err error)

	// NewRequest returns a standard FastCGI request
	// with a unique request ID allocted by the client
	NewRequest() *Request

	// AllocID allocates a new reqID.
	// It blocks if all possible uint16 IDs are allocated.
	AllocID() uint16

	// ReleaseID releases a reqID.
	// It never blocks.
	ReleaseID(uint16)
}

// NewClient returns a Client of the given
// connection (net.Conn)
func NewClient(conn net.Conn) Client {
	cid := make(chan uint16)
	go func() {
		for i := uint16(0); i < 65535; i++ {
			cid <- i
		}
		cid <- uint16(65535)
	}()

	return &client{
		conn:   newConn(conn),
		ids:    make(map[uint16]bool),
		chanID: cid,
	}
}