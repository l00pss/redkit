package redkit

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ConnState represents the state of a client connection
type ConnState int

const (
	StateNew ConnState = iota
	StateActive
	StateIdle
	StateClosed
)

// RedisValue represents different types of Redis values
type RedisValue struct {
	Type  RedisType
	Str   string
	Int   int64
	Bulk  []byte
	Array []RedisValue
}

// RedisType represents Redis protocol data types
type RedisType int

const (
	SimpleString RedisType = iota
	ErrorReply
	Integer
	BulkString
	Array
	Null
)

// Command represents a Redis command with arguments
type Command struct {
	Name string
	Args []string
	Raw  []RedisValue
}

// CommandHandler defines the interface for handling Redis commands
type CommandHandler interface {
	Handle(conn *Connection, cmd *Command) RedisValue
}

// CommandHandlerFunc enables using functions as CommandHandler implementations
type CommandHandlerFunc func(conn *Connection, cmd *Command) RedisValue

// Handle implements CommandHandler interface for function types
func (f CommandHandlerFunc) Handle(conn *Connection, cmd *Command) RedisValue {
	return f(conn, cmd)
}

// Server represents the Redis-compatible server
type Server struct {
	Address        string
	TLSConfig      *tls.Config
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxConnections int
	ErrorLog       *log.Logger
	ConnStateHook  func(net.Conn, ConnState)

	handlers    map[string]CommandHandler
	listener    net.Listener
	activeConns map[*Connection]struct{}
	connCount   atomic.Int64
	inShutdown  atomic.Bool
	mu          sync.RWMutex
	onShutdown  []func()
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}
