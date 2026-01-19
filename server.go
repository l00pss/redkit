package redkit

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

func NewServer(address string) *Server {
	config := DefaultServerConfig()
	config.Address = address
	return NewServerWithConfig(config)
}

func NewServerWithConfig(config *ServerConfig) *Server {
	if config == nil {
		config = DefaultServerConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	if config.Logger == nil {
		config.Logger = NewDefaultLogger(nil, LogLevelInfo)
	}

	server := &Server{
		Address:            config.Address,
		TLSConfig:          config.TLSConfig,
		ReadTimeout:        config.ReadTimeout,
		WriteTimeout:       config.WriteTimeout,
		IdleTimeout:        config.IdleTimeout,
		IdleCheckFrequency: config.IdleCheckFrequency,
		MaxConnections:     config.MaxConnections,
		Logger:             config.Logger,
		ConnStateHook:      config.ConnStateHook,
		handlers:           make(map[string]CommandHandler),
		middlewareChain:    NewMiddlewareChain(),
		activeConns:        make(map[*Connection]struct{}),
		ctx:                ctx,
		cancel:             cancel,
	}

	server.registerDefaultHandlers()
	server.startIdleChecker()

	return server
}

// RegisterCommand registers a command handler
func (s *Server) RegisterCommand(name string, handler CommandHandler) error {
	if name == "" || handler == nil {
		return fmt.Errorf("empty command name")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[strings.ToUpper(name)] = handler
	return nil
}

// RegisterCommandFunc registers a function as a command handler
func (s *Server) RegisterCommandFunc(name string, handler func(*Connection, *Command) RedisValue) error {
	if name == "" || handler == nil {
		return fmt.Errorf("empty command name")
	}
	return s.RegisterCommand(name, CommandHandlerFunc(handler))
}

// Use adds a middleware to the server's middleware chain
func (s *Server) Use(middleware Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.middlewareChain.Add(middleware)
}

// UseFunc adds a middleware function to the server's middleware chain
func (s *Server) UseFunc(fn func(*Connection, *Command, CommandHandler) RedisValue) {
	s.Use(MiddlewareFunc(fn))
}

// Listen starts listening on the configured address
func (s *Server) Listen() error {
	var err error
	if s.TLSConfig != nil {
		s.listener, err = tls.Listen("tcp", s.Address, s.TLSConfig)
	} else {
		s.listener, err = net.Listen("tcp", s.Address)
	}

	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.Address, err)
	}

	s.Logger.Info("Server listening on %s", s.Address)
	return nil
}

// Serve starts accepting connections (blocking)
func (s *Server) Serve() error {
	if s.listener == nil {
		if err := s.Listen(); err != nil {
			return err
		}
	}

	defer s.listener.Close()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.inShutdown.Load() {
				return nil
			}
			s.Logger.Error("Accept error: %v", err)
			continue
		}

		shouldHandle := true

		if s.MaxConnections > 0 {
			for {
				current := s.connCount.Load()
				if current >= int64(s.MaxConnections) {
					conn.Close()
					s.Logger.Warn("Connection limit reached, rejecting connection from %s", conn.RemoteAddr())
					shouldHandle = false
					break
				}
				if s.connCount.CompareAndSwap(current, current+1) {
					break
				}
			}
		} else {
			s.connCount.Add(1)
		}

		if shouldHandle {
			s.wg.Add(1)

			go func(netConn net.Conn) {
				defer s.wg.Done()
				defer s.connCount.Add(-1)

				s.handleConnectionInternal(netConn)
			}(conn)
		}
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.inShutdown.Store(true)
	s.cancel()

	// Close listener
	if s.listener != nil {
		err := s.listener.Close()
		if err != nil {
			return err
		}
	}

	// Close all active connections
	s.mu.RLock()
	conns := make([]*Connection, 0, len(s.activeConns))
	for conn := range s.activeConns {
		conns = append(conns, conn)
	}
	s.mu.RUnlock()

	var firstErr error
	for _, conn := range conns {
		if err := conn.Close(); err != nil && firstErr == nil {
			firstErr = err
			s.Logger.Warn("Error closing connection during shutdown: %v", err)
		}
	}

	// Run shutdown hooks
	s.mu.Lock()
	for _, fn := range s.onShutdown {
		fn()
	}
	s.mu.Unlock()

	// Wait for all connections to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return firstErr
	}
}

// handleConnectionInternal handles a single client connection
func (s *Server) handleConnectionInternal(netConn net.Conn) {

	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()

	conn := &Connection{
		conn:     netConn,
		reader:   bufio.NewReader(netConn),
		writer:   bufio.NewWriter(netConn),
		server:   s,
		ctx:      ctx,
		cancel:   cancel,
		lastUsed: time.Now(),
	}

	conn.setState(StateNew)

	s.mu.Lock()
	s.activeConns[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.activeConns, conn)
		s.mu.Unlock()
	}()

	conn.setState(StateActive)

	s.Logger.Debug("New connection from %s", netConn.RemoteAddr())

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if s.ReadTimeout > 0 {
			if err := netConn.SetReadDeadline(time.Now().Add(s.ReadTimeout)); err != nil {
				s.Logger.Error("Failed to set read deadline: %v", err)
				return
			}
		}

		cmd, err := conn.readCommand()
		if err != nil {
			errStr := err.Error()
			if err == io.EOF || strings.Contains(errStr, "use of closed network connection") {
				s.Logger.Debug("Connection closed by %s", netConn.RemoteAddr())
			} else {
				s.Logger.Error("Error reading command from %s: %v", netConn.RemoteAddr(), err)
			}
			return
		}

		conn.mu.Lock()
		conn.lastUsed = time.Now()
		conn.mu.Unlock()

		s.Logger.Debug("Command from %s: %s %v", netConn.RemoteAddr(), cmd.Name, cmd.Args)

		conn.setState(StateProcessing)
		response := s.handleCommand(conn, cmd)
		conn.setState(StateActive)

		if s.WriteTimeout > 0 {
			err := netConn.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
			if err != nil {
				return
			}
		}

		if err := conn.writeValue(response); err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				s.Logger.Debug("Connection closed while writing to %s", netConn.RemoteAddr())
			} else {
				s.Logger.Error("Error writing response to %s: %v", netConn.RemoteAddr(), err)
			}
			return
		}

		if err := conn.writer.Flush(); err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				s.Logger.Debug("Connection closed while flushing to %s", netConn.RemoteAddr())
			} else {
				s.Logger.Error("Error flushing response to %s: %v", netConn.RemoteAddr(), err)
			}
			return
		}
	}
}

// handleCommand processes a Redis command
func (s *Server) handleCommand(conn *Connection, cmd *Command) RedisValue {
	defer func() {
		if r := recover(); r != nil {
			s.Logger.Error("PANIC in command handler '%s': %v", cmd.Name, r)
		}
	}()

	if cmd == nil || cmd.Name == "" {
		return RedisValue{
			Type: ErrorReply,
			Str:  "ERR empty command",
		}
	}

	s.mu.RLock()
	handler, exists := s.handlers[strings.ToUpper(cmd.Name)]
	s.mu.RUnlock()

	if !exists {
		return RedisValue{
			Type: ErrorReply,
			Str:  fmt.Sprintf("ERR unknown command '%s'", cmd.Name),
		}
	}

	// Execute through middleware chain
	return s.middlewareChain.Execute(conn, cmd, handler)
}

// OnShutdown registers a function to call on shutdown
func (s *Server) OnShutdown(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onShutdown = append(s.onShutdown, f)
}

// GetActiveConnections returns the number of active connections
func (s *Server) GetActiveConnections() int64 {
	return s.connCount.Load()
}

// IsShutdown returns whether the server is shutting down
func (s *Server) IsShutdown() bool {
	return s.inShutdown.Load()
}

// TriggerIdleCheck manually triggers idle connection checking
func (s *Server) TriggerIdleCheck() {
	s.checkIdleConnections()
}

// startIdleChecker starts a background goroutine to check for idle connections
func (s *Server) startIdleChecker() {
	go func() {
		checkInterval := s.IdleCheckFrequency
		if checkInterval <= 0 {
			checkInterval = 30 * time.Second
		}
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.checkIdleConnections()
			}
		}
	}()
}

// checkIdleConnections checks all active connections for idle timeout
func (s *Server) checkIdleConnections() {
	if s.IdleTimeout <= 0 {
		return // Idle timeout disabled
	}

	now := time.Now()
	idleThreshold := now.Add(-s.IdleTimeout)

	s.mu.RLock()
	connsToCheck := make([]*Connection, 0, len(s.activeConns))
	for conn := range s.activeConns {
		connsToCheck = append(connsToCheck, conn)
	}
	s.mu.RUnlock()

	// Check each connection for idle timeout
	var idleConns []*Connection
	for _, conn := range connsToCheck {
		conn.mu.RLock()
		lastUsed := conn.lastUsed
		conn.mu.RUnlock()

		currentState := ConnState(conn.state.Load())

		if (currentState == StateActive || currentState == StateIdle) && lastUsed.Before(idleThreshold) {
			idleConns = append(idleConns, conn)
		}
	}

	for _, conn := range idleConns {
		s.Logger.Info("Closing idle connection %s", conn.RemoteAddr())
		conn.Close()
	}
}

// setConnectionActive sets a connection to active state
func (s *Server) setConnectionActive(conn *Connection) {
	currentState := ConnState(conn.state.Load())
	if currentState == StateIdle {
		conn.setState(StateActive)
		if s.ConnStateHook != nil {
			s.ConnStateHook(conn.conn, StateActive)
		}
	}
}
