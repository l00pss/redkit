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

type CommandHandler interface {
	Handle(conn *Connection, cmd *Command) RedisValue
}

type CommandHandlerFunc func(conn *Connection, cmd *Command) RedisValue

func (f CommandHandlerFunc) Handle(conn *Connection, cmd *Command) RedisValue {
	return f(conn, cmd)
}

type Middleware interface {
	Handle(conn *Connection, cmd *Command, next CommandHandler) RedisValue
}

type MiddlewareFunc func(conn *Connection, cmd *Command, next CommandHandler) RedisValue

func (f MiddlewareFunc) Handle(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
	return f(conn, cmd, next)
}

type MiddlewareChain struct {
	middlewares []Middleware
}

func NewMiddlewareChain() *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: make([]Middleware, 0),
	}
}

func (mc *MiddlewareChain) Add(middleware Middleware) *MiddlewareChain {
	mc.middlewares = append(mc.middlewares, middleware)
	return mc
}

func (mc *MiddlewareChain) Execute(conn *Connection, cmd *Command, handler CommandHandler) RedisValue {
	if len(mc.middlewares) == 0 {
		return handler.Handle(conn, cmd)
	}

	final := handler

	for i := len(mc.middlewares) - 1; i >= 0; i-- {
		mw := mc.middlewares[i]
		next := final
		final = &wrappedHandler{
			middleware: mw,
			next:       next,
		}
	}

	return final.Handle(conn, cmd)
}

type wrappedHandler struct {
	middleware Middleware
	next       CommandHandler
}

func (wh *wrappedHandler) Handle(conn *Connection, cmd *Command) RedisValue {
	return wh.middleware.Handle(conn, cmd, wh.next)
}

func (mc *MiddlewareChain) Handler(handler CommandHandler) CommandHandler {
	return CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
		return mc.Execute(conn, cmd, handler)
	})
}

type ConnState int

const (
	StateNew ConnState = iota
	StateActive
	StateIdle
	StateClosed
)

type RedisValue struct {
	Type  RedisType
	Str   string
	Int   int64
	Bulk  []byte
	Array []RedisValue
}

type RedisType int

const (
	SimpleString RedisType = iota
	ErrorReply
	Integer
	BulkString
	Array
	Null
)

type Command struct {
	Name string
	Args []string
	Raw  []RedisValue
}

type ServerConfig struct {
	Address        string
	TLSConfig      *tls.Config
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxConnections int
	ErrorLog       *log.Logger
	ConnStateHook  func(net.Conn, ConnState)
}

func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Address:        ":6379",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxConnections: 1000,
		ErrorLog:       log.New(log.Writer(), "[RedKit] ", log.LstdFlags),
	}
}

type Server struct {
	Address        string
	TLSConfig      *tls.Config
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	MaxConnections int
	ErrorLog       *log.Logger
	ConnStateHook  func(net.Conn, ConnState)

	handlers        map[string]CommandHandler
	middlewareChain *MiddlewareChain
	listener        net.Listener
	activeConns     map[*Connection]struct{}
	connCount       atomic.Int64
	inShutdown      atomic.Bool
	mu              sync.RWMutex
	onShutdown      []func()
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}
