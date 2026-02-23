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

type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelOff
)

type Logger interface {
	Debug(format string, v ...interface{})
	Info(format string, v ...interface{})
	Warn(format string, v ...interface{})
	Error(format string, v ...interface{})
}

type defaultLogger struct {
	logger *log.Logger
	level  LogLevel
}

func NewDefaultLogger(logger *log.Logger, level LogLevel) Logger {
	if logger == nil {
		logger = log.New(log.Writer(), "[RedKit] ", log.LstdFlags)
	}
	return &defaultLogger{logger: logger, level: level}
}

func (l *defaultLogger) Debug(format string, v ...interface{}) {
	if l.level <= LogLevelDebug {
		l.logger.Printf("[DEBUG] "+format, v...)
	}
}

func (l *defaultLogger) Info(format string, v ...interface{}) {
	if l.level <= LogLevelInfo {
		l.logger.Printf("[INFO] "+format, v...)
	}
}

func (l *defaultLogger) Warn(format string, v ...interface{}) {
	if l.level <= LogLevelWarn {
		l.logger.Printf("[WARN] "+format, v...)
	}
}

func (l *defaultLogger) Error(format string, v ...interface{}) {
	if l.level <= LogLevelError {
		l.logger.Printf("[ERROR] "+format, v...)
	}
}

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
	middlewares atomic.Value // []Middleware
	mu          sync.Mutex
}

func NewMiddlewareChain() *MiddlewareChain {
	mc := &MiddlewareChain{}
	mc.middlewares.Store([]Middleware{})
	return mc
}

func (mc *MiddlewareChain) Add(middleware Middleware) *MiddlewareChain {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	current := mc.middlewares.Load().([]Middleware)
	newMiddlewares := make([]Middleware, len(current)+1)
	copy(newMiddlewares, current)
	newMiddlewares[len(current)] = middleware
	mc.middlewares.Store(newMiddlewares)
	
	return mc
}

func (mc *MiddlewareChain) Execute(conn *Connection, cmd *Command, handler CommandHandler) RedisValue {
	middlewares := mc.middlewares.Load().([]Middleware)

	if len(middlewares) == 0 {
		return handler.Handle(conn, cmd)
	}

	final := handler

	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
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
	StateProcessing
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
	Address            string
	TLSConfig          *tls.Config
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	IdleCheckFrequency time.Duration
	MaxConnections     int
	Logger             Logger
	ConnStateHook      func(net.Conn, ConnState)
}

func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Address:            ":6379",
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       30 * time.Second,
		IdleTimeout:        120 * time.Second,
		IdleCheckFrequency: 30 * time.Second,
		MaxConnections:     1000,
		Logger:             NewDefaultLogger(nil, LogLevelInfo),
	}
}

type Server struct {
	Address            string
	TLSConfig          *tls.Config
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	IdleCheckFrequency time.Duration
	MaxConnections     int
	Logger             Logger
	ConnStateHook      func(net.Conn, ConnState)

	handlers        *sync.Map // map[string]CommandHandler
	middlewareChain *MiddlewareChain
	listener        net.Listener
	activeConns     *sync.Map // map[*Connection]struct{}
	connCount       atomic.Int64
	inShutdown      atomic.Bool
	mu              sync.RWMutex
	onShutdown      []func()
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}
