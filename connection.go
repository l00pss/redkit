package redkit

import (
	"bufio"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Connection represents a client connection to the Redis server
type Connection struct {
	conn      net.Conn
	reader    *bufio.Reader
	writer    *bufio.Writer
	server    *Server
	state     atomic.Int32
	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
	lastUsed  time.Time
}

// setState updates the connection state
func (c *Connection) setState(state ConnState) {
	c.state.Store(int32(state))
	if c.server.ConnStateHook != nil {
		c.server.ConnStateHook(c.conn, state)
	}
}

// Close closes the connection
func (c *Connection) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.setState(StateClosed)
		c.cancel()
		err = c.conn.Close()
	})
	return err
}

// GetState returns the current connection state
func (c *Connection) GetState() ConnState {
	return ConnState(c.state.Load())
}

// RemoteAddr returns the remote network address
func (c *Connection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// LocalAddr returns the local network address
func (c *Connection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}
