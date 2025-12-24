package redkit

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

// NewServer creates a new Redis-compatible server instance
func NewServer(address string) *Server {
	ctx, cancel := context.WithCancel(context.Background())

	server := &Server{
		Address:        address,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxConnections: 1000,
		ErrorLog:       log.New(log.Writer(), "[RedKit] ", log.LstdFlags),
		handlers:       make(map[string]CommandHandler),
		activeConns:    make(map[*Connection]struct{}),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Register default handlers
	server.registerDefaultHandlers()

	// Start idle connection checker
	server.startIdleChecker()

	return server
}

// RegisterCommand registers a command handler
func (s *Server) RegisterCommand(name string, handler CommandHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[strings.ToUpper(name)] = handler
}

// RegisterCommandFunc registers a command handler function
func (s *Server) RegisterCommandFunc(name string, handler func(*Connection, *Command) RedisValue) {
	s.RegisterCommand(name, CommandHandlerFunc(handler))
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

	s.ErrorLog.Printf("RedKit server listening on %s", s.Address)
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
			s.ErrorLog.Printf("Accept error: %v", err)
			continue
		}

		// Check connection limit
		if s.MaxConnections > 0 && s.connCount.Load() >= int64(s.MaxConnections) {
			err := conn.Close()
			if err != nil {
				return err
			}
			s.ErrorLog.Printf("Connection limit reached, rejecting connection from %s", conn.RemoteAddr())
			continue
		}

		s.wg.Add(1)
		go s.handleConnection(conn)
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
	for conn := range s.activeConns {
		err := conn.Close()
		if err != nil {
			return err
		}
	}
	s.mu.RUnlock()

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
		return nil
	}
}

// handleConnection handles a single client connection
func (s *Server) handleConnection(netConn net.Conn) {
	defer s.wg.Done()

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

	conn.state.Store(int32(StateNew))
	s.connCount.Add(1)

	// Add to active connections
	s.mu.Lock()
	s.activeConns[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		conn.Close()
		s.connCount.Add(-1)
		s.mu.Lock()
		delete(s.activeConns, conn)
		s.mu.Unlock()
	}()

	// Call state hook
	if s.ConnStateHook != nil {
		s.ConnStateHook(netConn, StateNew)
	}

	conn.setState(StateActive)
	if s.ConnStateHook != nil {
		s.ConnStateHook(netConn, StateActive)
	}

	// Handle commands
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set read timeout
		if s.ReadTimeout > 0 {
			netConn.SetReadDeadline(time.Now().Add(s.ReadTimeout))
		}

		// Parse command
		cmd, err := conn.readCommand()
		if err != nil {
			if err != io.EOF {
				s.ErrorLog.Printf("Error reading command from %s: %v", netConn.RemoteAddr(), err)
			}
			return
		}

		conn.lastUsed = time.Now()

		// Set connection to active if it was idle
		s.setConnectionActive(conn)

		// Handle command
		response := s.handleCommand(conn, cmd)

		// Set write timeout
		if s.WriteTimeout > 0 {
			err := netConn.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
			if err != nil {
				return
			}
		}

		// Send response
		if err := conn.writeValue(response); err != nil {
			s.ErrorLog.Printf("Error writing response to %s: %v", netConn.RemoteAddr(), err)
			return
		}

		if err := conn.writer.Flush(); err != nil {
			s.ErrorLog.Printf("Error flushing response to %s: %v", netConn.RemoteAddr(), err)
			return
		}
	}
}

// handleCommand processes a Redis command
func (s *Server) handleCommand(conn *Connection, cmd *Command) RedisValue {
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

	return handler.Handle(conn, cmd)
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

// TriggerIdleCheck manually triggers idle connection checking (for testing)
func (s *Server) TriggerIdleCheck() {
	s.checkIdleConnections()
}

// startIdleChecker starts a background goroutine to check for idle connections
func (s *Server) startIdleChecker() {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
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
	var idleConns []*Connection
	for conn := range s.activeConns {
		conn.mu.RLock()
		lastUsed := conn.lastUsed
		currentState := ConnState(conn.state.Load())
		conn.mu.RUnlock()

		// If connection is active but hasn't been used recently, mark as idle
		if currentState == StateActive && lastUsed.Before(idleThreshold) {
			idleConns = append(idleConns, conn)
		}
	}
	s.mu.RUnlock()

	// Set connections to idle state
	for _, conn := range idleConns {
		conn.setState(StateIdle)
		s.ErrorLog.Printf("Connection %s marked as idle", conn.RemoteAddr())
	}
}

// setConnectionActive sets a connection to active state (used when receiving commands)
func (s *Server) setConnectionActive(conn *Connection) {
	currentState := ConnState(conn.state.Load())
	if currentState == StateIdle {
		conn.setState(StateActive)
		if s.ConnStateHook != nil {
			s.ConnStateHook(conn.conn, StateActive)
		}
	}
}
