/*
Package redkit provides core type definitions for a Redis-compatible server implementation.

This file defines the fundamental data structures and interfaces used throughout
the Redis server implementation, including:

Core Types:
- ConnState: Client connection state management
- RedisValue: Redis protocol value representation
- RedisType: Redis protocol data type constants
- Command: Redis command structure with arguments
- CommandHandler: Interface for command processing
- Server: Main server configuration and state

Connection Management:
The ConnState type tracks client connection lifecycle from initial connection
through active usage to graceful shutdown.

Protocol Support:
RedisValue and RedisType provide complete RESP (Redis Serialization Protocol)
support for all standard Redis data types including strings, integers, arrays,
and error responses.

Command Processing:
The Command struct parses incoming Redis commands while CommandHandler interface
enables flexible command implementation and registration.

Server Architecture:
The Server struct encapsulates all configuration, connection management, and
command routing functionality with support for TLS, timeouts, connection limits,
and graceful shutdown.

Usage Example:

	server := &redkit.Server{
		Address:        ":6379",
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxConnections: 1000,
		ConnStateHook: func(conn net.Conn, state ConnState) {
			log.Printf("Connection %s state changed to %v",
				conn.RemoteAddr(), state)
		},
	}

	server.RegisterCommandFunc("GET", myGetHandler)
	log.Fatal(server.ListenAndServe())
*/
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

/*
Connection State Management

ConnState tracks the lifecycle of client connections to enable proper
resource management and monitoring. State transitions follow this pattern:

StateNew -> StateActive -> StateIdle -> StateClosed
                     ↑         ↓
                     └─────────┘
                   (can cycle between Active/Idle)

State hooks can be registered to monitor transitions for logging,
metrics collection, or custom connection management logic.
*/

// ConnState represents the state of a client connection
type ConnState int

const (
	StateNew    ConnState = iota // Initial connection established
	StateActive                  // Connection actively processing commands
	StateIdle                    // Connection idle, waiting for commands
	StateClosed                  // Connection terminated and cleaned up
)

/*
Redis Protocol Value Representation

RedisValue encapsulates all possible Redis protocol data types in a single
struct. The Type field determines which field contains the actual value:

- SimpleString: Use Str field (e.g., "OK", "PONG")
- ErrorReply: Use Str field (e.g., "ERR invalid command")
- Integer: Use Int field (e.g., 42, -1)
- BulkString: Use Bulk field (e.g., []byte("hello"))
- Array: Use Array field (e.g., []RedisValue{...})
- Null: No additional data needed

This design enables efficient value passing and type-safe Redis response
construction throughout the server implementation.
*/

// RedisValue represents different types of Redis values
type RedisValue struct {
	Type  RedisType    // The Redis protocol type
	Str   string       // Used for SimpleString and ErrorReply
	Int   int64        // Used for Integer values
	Bulk  []byte       // Used for BulkString (binary-safe)
	Array []RedisValue // Used for Array of values
}

/*
Redis Protocol Data Types

RedisType constants correspond to RESP (Redis Serialization Protocol) data types.
Each type has a specific wire format and use case:

- SimpleString: Single-line strings without newlines (status replies)
- ErrorReply: Error messages with optional error codes
- Integer: 64-bit signed integers
- BulkString: Binary-safe strings with explicit length
- Array: Ordered collections of Redis values
- Null: Represents absence of data or null values

These types enable full RESP protocol compliance and interoperability
with all standard Redis clients.
*/

// RedisType represents Redis protocol data types
type RedisType int

const (
	SimpleString RedisType = iota // Status replies like "OK", "PONG"
	ErrorReply                    // Error messages like "ERR unknown command"
	Integer                       // 64-bit signed integers
	BulkString                    // Binary-safe strings with length prefix
	Array                         // Ordered collections of Redis values
	Null                          // Null values (empty responses)
)

/*
Redis Command Representation

Command encapsulates a parsed Redis command with its arguments.
Commands are typically parsed from client input following RESP format.

Fields:
- Name: The command name (e.g., "GET", "SET", "HGET")
- Args: Parsed string arguments (excluding command name)
- Raw: Original RedisValue array from protocol parsing

Example command parsing:
	Input: ["SET", "key", "value"]
	Result: Command{
		Name: "SET",
		Args: ["key", "value"],
		Raw:  [RedisValue{Type: BulkString, Bulk: []byte("SET")}, ...]
	}

This structure enables efficient command routing and argument access
while preserving original protocol data for advanced use cases.
*/

// Command represents a Redis command with arguments
type Command struct {
	Name string       // Command name (always uppercase)
	Args []string     // Command arguments (excluding command name)
	Raw  []RedisValue // Original parsed values from protocol
}

/*
Command Handler Interface

CommandHandler defines the contract for processing Redis commands.
Handlers receive the client connection context and parsed command,
then return a RedisValue response to send back to the client.

The interface design enables:
- Flexible command implementation (structs or functions)
- Access to connection context for state management
- Type-safe response construction
- Easy testing and mocking

Implementation patterns:

1. Function handlers (most common):
	func myHandler(conn *Connection, cmd *Command) RedisValue {
		return RedisValue{Type: SimpleString, Str: "OK"}
	}

2. Struct handlers (for stateful commands):
	type MyHandler struct { state map[string]string }
	func (h *MyHandler) Handle(conn *Connection, cmd *Command) RedisValue {
		// Implementation with access to h.state
	}
*/

// CommandHandler defines the interface for handling Redis commands
type CommandHandler interface {
	// Handle processes a Redis command and returns the response
	// conn: Client connection context for state access
	// cmd: Parsed command with arguments
	// Returns: RedisValue response to send to client
	Handle(conn *Connection, cmd *Command) RedisValue
}

// CommandHandlerFunc enables using functions as CommandHandler implementations
// This adapter pattern allows registering functions directly as handlers
// without needing to create wrapper structs
type CommandHandlerFunc func(conn *Connection, cmd *Command) RedisValue

// Handle implements CommandHandler interface for function types
func (f CommandHandlerFunc) Handle(conn *Connection, cmd *Command) RedisValue {
	return f(conn, cmd)
}

/*
Redis-Compatible Server Configuration and State

Server encapsulates all functionality needed to run a Redis-compatible server.
It manages connections, routes commands, and provides extensive configuration
options for production deployments.

Configuration Fields:
- Network: Address and TLS configuration
- Timeouts: Read, Write, and Idle timeout settings
- Limits: Maximum connection limits and resource constraints
- Monitoring: Error logging and connection state hooks

Runtime State:
- Connection tracking and lifecycle management
- Command handler registry and routing
- Graceful shutdown coordination
- Thread-safe operations with proper synchronization

Key Features:
- Full Redis protocol (RESP) compatibility
- TLS/SSL support for secure connections
- Configurable timeouts and connection limits
- Connection state monitoring and hooks
- Graceful shutdown with connection draining
- Thread-safe concurrent operation
- Extensible command handler system

Production Considerations:
- Set appropriate timeouts to prevent resource leaks
- Configure MaxConnections based on system resources
- Use TLS in production environments
- Implement ConnStateHook for monitoring and metrics
- Handle shutdown signals for graceful termination
*/

// Server represents the Redis-compatible server
type Server struct {
	// Network Configuration
	Address   string      // Server bind address (e.g., ":6379", "127.0.0.1:6379")
	TLSConfig *tls.Config // Optional TLS configuration for secure connections

	// Timeout Configuration
	ReadTimeout  time.Duration // Maximum time to wait for client requests
	WriteTimeout time.Duration // Maximum time to wait for response writes
	IdleTimeout  time.Duration // Maximum time to keep idle connections open

	// Resource Limits
	MaxConnections int // Maximum number of concurrent client connections

	// Monitoring and Logging
	ErrorLog      *log.Logger               // Error logging destination
	ConnStateHook func(net.Conn, ConnState) // Connection state change callback

	// Command Processing
	handlers map[string]CommandHandler // Registered command handlers

	// Server Runtime State (internal)
	listener    net.Listener             // Network listener
	activeConns map[*Connection]struct{} // Active connection tracking
	connCount   atomic.Int64             // Current connection count (atomic)
	inShutdown  atomic.Bool              // Shutdown flag (atomic)
	mu          sync.RWMutex             // Protects shared state
	onShutdown  []func()                 // Shutdown callback functions
	ctx         context.Context          // Server context for cancellation
	cancel      context.CancelFunc       // Context cancellation function
	wg          sync.WaitGroup           // Wait group for goroutine coordination
}
