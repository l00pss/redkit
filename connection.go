/*
Package redkit implements client connection management for Redis-compatible servers.

This file provides the Connection type and associated methods for managing
individual client connections throughout their lifecycle.

Core Responsibilities:
- TCP/TLS connection wrapping and management
- Buffered I/O for optimal Redis protocol performance
- Thread-safe connection state tracking and transitions
- Context-based cancellation and resource cleanup
- Connection metadata and network address access

Connection Lifecycle:
1. Connection creation and initialization (StateNew)
2. Active command processing (StateActive)
3. Idle waiting between commands (StateIdle)
4. Graceful termination and cleanup (StateClosed)

Thread Safety:
The Connection type is designed for concurrent access with proper synchronization:
- Atomic operations for state management
- Mutex protection for shared fields (lastUsed)
- sync.Once for safe connection cleanup
- Context cancellation for coordinated shutdown

Performance Optimizations:
- Buffered readers and writers to minimize syscalls
- Atomic state tracking to avoid lock contention
- Efficient network address caching
- Minimal allocation overhead in hot paths

Integration:
Connection instances are typically created by the Server during client
connection acceptance and managed throughout the command processing
lifecycle. They provide the necessary context for command handlers
to access client-specific state and network information.

Usage Example:

	// Connection is typically created by the server
	conn := &Connection{
		conn:     netConn,
		reader:   bufio.NewReader(netConn),
		writer:   bufio.NewWriter(netConn),
		server:   server,
		ctx:      ctx,
		cancel:   cancelFunc,
		lastUsed: time.Now(),
	}

	// Command handlers can access connection context
	func myHandler(conn *Connection, cmd *Command) RedisValue {
		clientAddr := conn.RemoteAddr()
		state := conn.GetState()
		// Process command with connection context
	}
*/
package redkit

import (
	"bufio"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

/*
Client Connection Management

Connection wraps a network connection with Redis protocol-specific
functionality including buffered I/O, state tracking, and lifecycle management.

Fields Overview:
- conn: Underlying TCP/TLS network connection
- reader/writer: Buffered I/O for efficient Redis protocol handling
- server: Reference to parent server for configuration and hooks
- state: Atomic connection state (StateNew/Active/Idle/Closed)
- closeOnce: Ensures connection is closed exactly once (thread-safe)
- ctx/cancel: Context for coordinated cancellation and cleanup
- mu: Protects shared mutable fields like lastUsed
- lastUsed: Timestamp for idle timeout management

Thread Safety:
All public methods are thread-safe and can be called concurrently
from multiple goroutines. State transitions are atomic and properly
synchronized with connection hooks and cleanup operations.

Resource Management:
The Connection automatically manages its resources and integrates
with the server's connection tracking for proper cleanup during
shutdown scenarios.
*/

// Connection represents a client connection to the Redis server
type Connection struct {
	conn      net.Conn           // Underlying network connection
	reader    *bufio.Reader      // Buffered reader for efficient parsing
	writer    *bufio.Writer      // Buffered writer for response batching
	server    *Server            // Parent server reference
	state     atomic.Int32       // Current connection state (atomic)
	closeOnce sync.Once          // Ensures single cleanup execution
	ctx       context.Context    // Connection context for cancellation
	cancel    context.CancelFunc // Context cancellation function
	mu        sync.RWMutex       // Protects mutable fields
	lastUsed  time.Time          // Last activity timestamp for idle detection
}

/*
Connection State Management

These methods manage connection state transitions and provide
thread-safe access to connection status information.
*/

// setState updates the connection state
// Atomically updates the connection state and triggers the server's
// connection state hook if configured. This enables monitoring and
// metrics collection for connection lifecycle events.
//
// State transitions follow the pattern:
// StateNew -> StateActive -> StateIdle -> StateClosed
//
//	↑         ↓
//	└─────────┘ (can cycle)
//
// Parameters:
// - state: New connection state to set
func (c *Connection) setState(state ConnState) {
	c.state.Store(int32(state))
	if c.server.ConnStateHook != nil {
		c.server.ConnStateHook(c.conn, state)
	}
}

// Close closes the connection
// Performs thread-safe connection cleanup exactly once, regardless of
// how many times it's called. The cleanup process includes:
// 1. Setting connection state to StateClosed
// 2. Cancelling the connection context (stops background operations)
// 3. Closing the underlying network connection
//
// This method is safe to call multiple times and from multiple goroutines.
// Subsequent calls after the first will be no-ops.
//
// Returns:
// - error: Network connection close error, if any
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
// Thread-safe method to query the current state without triggering
// any state transitions or side effects. Useful for monitoring,
// logging, and conditional logic based on connection status.
//
// Returns:
// - ConnState: Current connection state (StateNew/Active/Idle/Closed)
func (c *Connection) GetState() ConnState {
	return ConnState(c.state.Load())
}

/*
Network Address Access

These methods provide access to the connection's network endpoints
for logging, monitoring, and access control purposes.
*/

// RemoteAddr returns the remote network address
// Provides access to the client's network address for logging,
// access control, and monitoring purposes. The address format
// depends on the network type (TCP: "IP:port", Unix: socket path).
//
// Returns:
// - net.Addr: Client's network address
func (c *Connection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// LocalAddr returns the local network address
// Provides access to the server's local address that accepted
// this connection. Useful for multi-interface servers and logging.
//
// Returns:
// - net.Addr: Server's local network address for this connection
func (c *Connection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}
