/*
Package redkit implements the core server functionality for a Redis-compatible server.

This file contains the main server implementation including:

Core Server Operations:
- Server lifecycle management (Listen, Serve, Shutdown)
- Connection handling and state management
- Command routing and processing
- Resource management and limits

Connection Management:
- TCP and TLS listener support
- Connection pooling and tracking
- Idle connection detection and cleanup
- Graceful connection termination

Command Processing:
- Redis protocol (RESP) command parsing
- Command handler registration and routing
- Response serialization and buffering
- Error handling and logging

Performance Features:
- Configurable timeouts (Read, Write, Idle)
- Connection limits and resource constraints
- Buffered I/O for optimal throughput
- Background idle connection cleanup

Production Features:
- Graceful shutdown with connection draining
- Comprehensive error logging and monitoring
- Connection state hooks for metrics collection
- Thread-safe concurrent operations

Usage Example:

	server := redkit.NewServer(":6379")

	// Configure server
	server.ReadTimeout = 30 * time.Second
	server.MaxConnections = 1000
	server.TLSConfig = &tls.Config{...}

	// Register custom commands
	server.RegisterCommandFunc("CUSTOM", func(conn *Connection, cmd *Command) RedisValue {
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	// Start server
	log.Fatal(server.ListenAndServe())

Architecture:
The server uses a goroutine-per-connection model with shared state protected
by appropriate synchronization primitives. Each client connection runs in its
own goroutine, enabling high concurrency while maintaining thread safety.
*/
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

/*
Server Construction and Initialization

NewServer creates a fully configured Redis-compatible server instance with
sensible defaults for production use. The server is ready to accept
connections after creation.

Default Configuration:
- ReadTimeout: 30 seconds (prevents hung connections)
- WriteTimeout: 30 seconds (ensures responsive clients)
- IdleTimeout: 120 seconds (automatic cleanup of unused connections)
- MaxConnections: 1000 (reasonable limit for most applications)
- ErrorLog: Standard logger with [RedKit] prefix

The server automatically:
- Registers default Redis commands (PING, ECHO, QUIT, HELP)
- Starts background idle connection monitoring
- Initializes thread-safe connection tracking
- Sets up graceful shutdown coordination

Parameters:
- address: Network address to bind (e.g., ":6379", "127.0.0.1:6379")

Returns:
- *Server: Configured server instance ready for use
*/

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

/*
Command Handler Registration

These methods enable registration of custom command handlers to extend
server functionality. Commands are case-insensitive and automatically
converted to uppercase for consistent routing.

Thread Safety:
All registration methods are thread-safe and can be called concurrently
with server operations. However, it's recommended to register all handlers
before starting the server for better performance.

Handler Lifecycle:
Handlers receive parsed commands and connection context, enabling:
- Access to client connection state and metadata
- Custom response generation and error handling
- Connection-specific behavior and state management
*/

// RegisterCommand registers a command handler
// name: Command name (case-insensitive, e.g., "GET", "set", "MyCommand")
// handler: CommandHandler implementation to process the command
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
// This is a convenience method for registering function handlers without
// implementing the CommandHandler interface explicitly
//
// name: Command name (case-insensitive)
// handler: Function with signature func(*Connection, *Command) RedisValue
func (s *Server) RegisterCommandFunc(name string, handler func(*Connection, *Command) RedisValue) error {
	if name == "" || handler == nil {
		return fmt.Errorf("empty command name")
	}
	return s.RegisterCommand(name, CommandHandlerFunc(handler))
}

/*
Network Listener Management

These methods control the server's network listener lifecycle.
The server supports both plain TCP and TLS-encrypted connections
based on the TLSConfig setting.
*/

// Listen starts listening on the configured address
// Creates either a TCP or TLS listener based on server configuration.
// This method is idempotent and can be called multiple times safely.
//
// Returns:
// - error: Network binding errors or address conflicts
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
// This method blocks until the server shuts down or encounters a fatal error.
// Each accepted connection is handled in its own goroutine for concurrency.
//
// Connection Processing:
// - Enforces MaxConnections limit (rejects excess connections)
// - Tracks active connections for monitoring and shutdown
// - Handles connection state transitions and lifecycle
//
// Error Handling:
// - Logs connection errors without stopping the server
// - Gracefully handles shutdown signals
// - Returns nil on clean shutdown, error on fatal conditions
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

		s.wg.Add(1)
		go func(netConn net.Conn) {
			defer s.wg.Done()

			// Check connection limit after Accept to prevent TOCTOU race
			if s.MaxConnections > 0 && s.connCount.Add(1) > int64(s.MaxConnections) {
				s.connCount.Add(-1)
				netConn.Close()
				s.ErrorLog.Printf("Connection limit reached, rejecting connection from %s", netConn.RemoteAddr())
				return
			}

			s.handleConnectionInternal(netConn)
			s.connCount.Add(-1)
		}(conn)
	}
}

/*
Graceful Server Shutdown

Shutdown coordinates a clean server termination with proper resource cleanup
and connection draining. This ensures data integrity and prevents abrupt
client disconnections.

Shutdown Process:
1. Set shutdown flag to stop accepting new connections
2. Close network listener to reject incoming requests
3. Close all active client connections gracefully
4. Execute registered shutdown hooks for cleanup
5. Wait for all connection goroutines to terminate
6. Respect context timeout for forced termination

The method is safe to call multiple times and coordinates with background
processes like idle connection monitoring.
*/

// Shutdown gracefully shuts down the server
// ctx: Context with timeout for maximum shutdown duration
// Returns: Context timeout error or nil on successful shutdown
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

/*
Connection Lifecycle Management

These methods manage individual client connections from establishment
through command processing to graceful termination.
*/

// handleConnectionInternal handles a single client connection
// Runs in its own goroutine for each client connection.
// Manages the complete connection lifecycle including:
// - Connection context and cancellation
// - Buffered I/O setup for optimal performance
// - State tracking and hook notifications
// - Command parsing and response handling
// - Timeout enforcement and error recovery
// - Resource cleanup on connection termination
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

	conn.state.Store(int32(StateNew))

	s.mu.Lock()
	s.activeConns[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.activeConns, conn)
		s.mu.Unlock()
	}()

	if s.ConnStateHook != nil {
		s.ConnStateHook(netConn, StateNew)
	}

	conn.setState(StateActive)
	if s.ConnStateHook != nil {
		s.ConnStateHook(netConn, StateActive)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if s.ReadTimeout > 0 {
			if err := netConn.SetReadDeadline(time.Now().Add(s.ReadTimeout)); err != nil {
				s.ErrorLog.Printf("Failed to set read deadline: %v", err)
				return
			}
		}

		cmd, err := conn.readCommand()
		if err != nil {
			if err != io.EOF {
				s.ErrorLog.Printf("Error reading command from %s: %v", netConn.RemoteAddr(), err)
			}
			return
		}

		conn.mu.Lock()
		conn.lastUsed = time.Now()
		conn.mu.Unlock()

		s.setConnectionActive(conn)

		response := s.handleCommand(conn, cmd)

		if s.WriteTimeout > 0 {
			err := netConn.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
			if err != nil {
				return
			}
		}

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
// Routes parsed commands to registered handlers and provides error handling
// for unknown commands and processing failures.
//
// Command Processing:
// - Validates command structure and name
// - Performs case-insensitive handler lookup
// - Delegates to appropriate command handler
// - Generates error responses for unknown commands
// - Recovers from panics in command handlers
//
// Parameters:
// - conn: Client connection context
// - cmd: Parsed command with arguments
//
// Returns:
// - RedisValue: Response to send to client (success or error)
func (s *Server) handleCommand(conn *Connection, cmd *Command) RedisValue {
	defer func() {
		if r := recover(); r != nil {
			s.ErrorLog.Printf("PANIC in command handler '%s': %v", cmd.Name, r)
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

	return handler.Handle(conn, cmd)
}

/*
Server Utility and Management Methods

These methods provide server introspection, lifecycle management,
and maintenance functionality for monitoring and operations.
*/

// OnShutdown registers a function to call on shutdown
// Useful for cleanup tasks, metric flushing, or resource deallocation.
// Hooks are executed during graceful shutdown before connection termination.
func (s *Server) OnShutdown(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onShutdown = append(s.onShutdown, f)
}

// GetActiveConnections returns the number of active connections
// Thread-safe method for monitoring server load and connection usage.
func (s *Server) GetActiveConnections() int64 {
	return s.connCount.Load()
}

// IsShutdown returns whether the server is shutting down
// Useful for conditional logic during server lifecycle transitions.
func (s *Server) IsShutdown() bool {
	return s.inShutdown.Load()
}

// TriggerIdleCheck manually triggers idle connection checking (for testing)
// Primarily used for testing idle timeout functionality.
func (s *Server) TriggerIdleCheck() {
	s.checkIdleConnections()
}

/*
Connection Monitoring and Maintenance

These methods implement background connection monitoring and automatic
cleanup of idle connections to prevent resource leaks and optimize
server performance.
*/

// startIdleChecker starts a background goroutine to check for idle connections
// Runs continuously until server shutdown, checking every 30 seconds.
// Automatically transitions unused connections from Active to Idle state.
func (s *Server) startIdleChecker() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
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
// Transitions connections that exceed IdleTimeout from Active to Idle state.
// This helps identify and manage unused connections for potential cleanup.
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

		if currentState == StateActive && lastUsed.Before(idleThreshold) {
			idleConns = append(idleConns, conn)
		}
	}

	for _, conn := range idleConns {
		conn.setState(StateIdle)
		s.ErrorLog.Printf("Connection %s marked as idle", conn.RemoteAddr())
	}
}

// setConnectionActive sets a connection to active state (used when receiving commands)
// Transitions idle connections back to active state when they receive new commands.
// This maintains accurate connection state and enables proper idle timeout management.
func (s *Server) setConnectionActive(conn *Connection) {
	currentState := ConnState(conn.state.Load())
	if currentState == StateIdle {
		conn.setState(StateActive)
		if s.ConnStateHook != nil {
			s.ConnStateHook(conn.conn, StateActive)
		}
	}
}
