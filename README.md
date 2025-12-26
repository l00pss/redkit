<br>
<div align="center">
  <img src="logo.png" alt="RedKit Logo" width="600"/>
  <br><br>
  
  [![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://golang.org/)
  [![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
  [![Test Status](https://img.shields.io/badge/Tests-Passing-green.svg)](#testing)
  [![Redis Compatible](https://img.shields.io/badge/Redis-Compatible-red.svg)](https://redis.io/)
</div>

A Redis-compatible server framework for Go 

##  Overview

RedKit is a lightweight, high-performance Redis-compatible server framework written in Go. It provides a foundation for building Redis-compatible applications and services, allowing you to implement custom Redis protocol handlers while maintaining compatibility with existing Redis clients and tools.

** Perfect for**: Custom Redis implementations, Redis proxies, specialized data stores, testing environments, and educational purposes.

##  Features

-  **Full Redis protocol compatibility** - Works with any Redis client (redis-cli, go-redis, jedis, etc.)
-  **High performance and low latency** - Built for speed (~14µs per operation)
-  **Easy to extend and customize** - Simple command registration system
-  **Built with Go** - Excellent concurrency support and memory safety
-  **Redis RESP Protocol** - Complete implementation of Redis Serialization Protocol
- ️ **TLS Support** - Secure connections out of the box
- ️ **Configurable timeouts and limits** - Fine-tune for your needs
-  **Connection state management** - Track connection lifecycle and idle states
-  **Comprehensive test coverage** - Tested with official Redis client (go-redis/redis)
- ️ **Full Redis compatibility** - Works seamlessly with redis-cli, go-redis, and other clients
-  **Advanced Redis commands** - INCR, DECR, MGET, MSET, EXPIRE, TTL, EXISTS, TYPE, KEYS
-  **Performance monitoring** - Built-in metrics and benchmarks

##  Quick Start

```bash
go get github.com/l00pss/redkit
```

### Basic Usage

```go
package main

import (
    "github.com/l00pss/redkit"
    "fmt"
)

func main() {
    // Create a new RedKit server
    server := redkit.NewServer(":6379")
    
    // Register a custom command
    server.RegisterCommandFunc("HELLO", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
        if len(cmd.Args) == 0 {
            return redkit.RedisValue{
                Type: redkit.SimpleString,
                Str:  "Hello from RedKit!",
            }
        }
        return redkit.RedisValue{
            Type: redkit.BulkString,
            Bulk: []byte(fmt.Sprintf("Hello, %s!", cmd.Args[0])),
        }
    })
    
    fmt.Println("Starting RedKit server on :6379...")
    if err := server.Serve(); err != nil {
        panic(err)
    }
}
```

### Advanced Example with Storage

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"
    "os"
    "os/signal"
    "syscall"
    
    "github.com/l00pss/redkit"
)

func main() {
    server := redkit.NewServer(":6379")
    
    // Thread-safe storage
    storage := make(map[string]string)
    mu := sync.RWMutex{}
    
    // SET command
    server.RegisterCommandFunc("SET", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
        if len(cmd.Args) != 2 {
            return redkit.RedisValue{
                Type: redkit.ErrorReply,
                Str:  "ERR wrong number of arguments for 'set' command",
            }
        }
        
        mu.Lock()
        storage[cmd.Args[0]] = cmd.Args[1]
        mu.Unlock()
        
        return redkit.RedisValue{Type: redkit.SimpleString, Str: "OK"}
    })
    
    // GET command
    server.RegisterCommandFunc("GET", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
        if len(cmd.Args) != 1 {
            return redkit.RedisValue{
                Type: redkit.ErrorReply,
                Str:  "ERR wrong number of arguments for 'get' command",
            }
        }
        
        mu.RLock()
        value, exists := storage[cmd.Args[0]]
        mu.RUnlock()
        
        if !exists {
            return redkit.RedisValue{Type: redkit.Null}
        }
        
        return redkit.RedisValue{
            Type: redkit.BulkString,
            Bulk: []byte(value),
        }
    })
    
    // Configure server
    server.ReadTimeout = 30 * time.Second
    server.WriteTimeout = 30 * time.Second
    server.IdleTimeout = 120 * time.Second
    server.MaxConnections = 1000
    
    // Graceful shutdown
    go func() {
        c := make(chan os.Signal, 1)
        signal.Notify(c, os.Interrupt, syscall.SIGTERM)
        <-c
        
        fmt.Println("\nShutting down server...")
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        
        if err := server.Shutdown(ctx); err != nil {
            log.Printf("Server shutdown error: %v", err)
        }
        fmt.Println("Server stopped")
        os.Exit(0)
    }()
    
    fmt.Println("Starting RedKit server...")
    log.Fatal(server.Serve())
}
```

##  Testing with Redis CLI

Once your server is running, you can test it with any Redis client:

```bash
# Using redis-cli
redis-cli -h localhost -p 6379

# Test basic commands
127.0.0.1:6379> PING
PONG

127.0.0.1:6379> HELLO
"Hello from RedKit!"

127.0.0.1:6379> HELLO world
"Hello, world!"

127.0.0.1:6379> SET mykey myvalue
OK

127.0.0.1:6379> GET mykey
"myvalue"

127.0.0.1:6379> GET nonexistent
(nil)

127.0.0.1:6379> DEL mykey
(integer) 1
```

##  API Reference

### Server Configuration

```go
type Server struct {
    Address         string        // Server address (default: ":6379")
    TLSConfig       *tls.Config   // TLS configuration (optional)
    ReadTimeout     time.Duration // Read timeout (default: 30s)
    WriteTimeout    time.Duration // Write timeout (default: 30s)
    IdleTimeout     time.Duration // Idle timeout (default: 120s)
    MaxConnections  int           // Max concurrent connections (default: 1000)
    ErrorLog        *log.Logger   // Error logger
    ConnStateHook   func(net.Conn, ConnState) // Connection state callback
}
```

### Redis Value Types

RedKit supports all Redis data types:

- `SimpleString` - Simple strings (+OK\r\n)
- `ErrorReply` - Error messages (-ERR ...\r\n)
- `Integer` - Integers (:123\r\n)
- `BulkString` - Binary-safe strings ($5\r\nhello\r\n)
- `Array` - Arrays of values (*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n)
- `Null` - Null values ($-1\r\n)

### Connection States

```go
const (
    StateNew     ConnState = iota // New connection
    StateActive                   // Processing commands
    StateIdle                     // Idle (no recent activity)
    StateClosed                   // Connection closed
)
```

### Command Registration

```go
// Function-based handler
server.RegisterCommandFunc("MYCOMMAND", func(conn *Connection, cmd *Command) RedisValue {
    // Your implementation here
    return RedisValue{Type: SimpleString, Str: "OK"}
})

// Interface-based handler
type MyHandler struct{}

func (h MyHandler) Handle(conn *Connection, cmd *Command) RedisValue {
    return RedisValue{Type: SimpleString, Str: "Custom handler"}
}

server.RegisterCommand("CUSTOM", MyHandler{})
```

##  Built-in Commands

RedKit comes with essential Redis commands:

### Basic Commands
- `PING` - Test connectivity (`PING` → `PONG`, `PING message` → `message`)
- `ECHO` - Echo messages (`ECHO hello` → `hello`)
- `QUIT` - Close connection gracefully

### String Operations
- `SET` - Set a key-value pair (`SET key value` → `OK`)
- `GET` - Get value by key (`GET key` → `value` or `(nil)`)
- `MSET` - Set multiple keys (`MSET key1 val1 key2 val2` → `OK`)
- `MGET` - Get multiple keys (`MGET key1 key2` → `[val1, val2]`)
- `SETNX` - Set if not exists (`SETNX key value` → `1` or `0`)

### Key Management
- `DEL` - Delete keys (`DEL key1 key2` → `2`)
- `EXISTS` - Check key existence (`EXISTS key1 key2` → `2`)
- `TYPE` - Get key type (`TYPE key` → `string` or `none`)
- `KEYS` - Find keys by pattern (`KEYS *` → `[key1, key2]`)
- `FLUSHDB` / `FLUSHALL` - Clear database (`FLUSHDB` → `OK`)

### Numeric Operations
- `INCR` - Increment integer (`INCR counter` → `1`)
- `DECR` - Decrement integer (`DECR counter` → `-1`)
- `INCRBY` - Increment by value (`INCRBY counter 5` → `5`)
- `DECRBY` - Decrement by value (`DECRBY counter 3` → `2`)

### Expiration
- `EXPIRE` - Set key expiration (`EXPIRE key 60` → `1`)
- `TTL` - Get time to live (`TTL key` → `59` or `-1` or `-2`)

##  Security & TLS

```go
import "crypto/tls"

server := redkit.NewServer(":6380")
server.TLSConfig = &tls.Config{
    // Your TLS configuration
    CertFile: "server.crt",
    KeyFile:  "server.key",
}
```

### Tested Scenarios
-  **5,000 concurrent operations** (50 goroutines × 100 operations)
-  **Data integrity** under concurrent access
-  **Error handling** and protocol compliance
-  **Connection state management**
-  **Idle connection detection and recovery**
-  **Graceful shutdown** with active connections

##  Comprehensive Testing

RedKit includes a complete test suite using the **official Redis Go client** (github.com/redis/go-redis/v9):

```bash
# Run all tests
go test -v

# Run specific test groups
go test -v -run TestBasicRedisCommands
go test -v -run TestStringOperations  
go test -v -run TestNumericOperations
go test -v -run TestConcurrentOperations

# Run benchmarks
go test -bench=. -benchtime=5s

# Test with race detector
go test -race -v

# Get test coverage
go test -cover -v
```

##  Performance Tips

1. **Use connection pooling** in your clients
2. **Set appropriate timeouts** based on your use case
3. **Limit concurrent connections** to prevent resource exhaustion
4. **Use bulk operations** when possible
5. **Monitor connection states** with ConnStateHook
6. **Configure idle timeouts** to free unused resources
7. **Enable TLS** only when needed (adds ~5-10µs latency)


##  Monitoring & Debugging

```go
// Track connection states
server.ConnStateHook = func(conn net.Conn, state ConnState) {
    log.Printf("Connection %s: %v", conn.RemoteAddr(), state)
}

// Monitor active connections
activeConns := server.GetActiveConnections()
log.Printf("Active connections: %d", activeConns)

// Check if server is shutting down
if server.IsShutdown() {
    log.Println("Server is shutting down")
}
```

##  Error Handling

RedKit provides comprehensive error handling:

```go
server.RegisterCommandFunc("VALIDATE", func(conn *Connection, cmd *Command) RedisValue {
    if len(cmd.Args) == 0 {
        return RedisValue{
            Type: ErrorReply,
            Str:  "ERR command requires at least one argument",
        }
    }
    
    if !isValid(cmd.Args[0]) {
        return RedisValue{
            Type: ErrorReply,
            Str:  "ERR invalid argument format",
        }
    }
    
    return RedisValue{Type: SimpleString, Str: "OK"}
})
```

##  Production Considerations

- **Memory Usage**: ~50MB baseline, scales with connections and data
- **CPU Usage**: Optimized for low CPU overhead
- **Network**: Supports high connection counts with proper limits
- **Monitoring**: Built-in metrics and state tracking
- **Logging**: Configurable error logging and debugging
- **Graceful Shutdown**: Proper cleanup of resources

##  Examples

Check the `/example` directory for more comprehensive examples and use cases:
- Basic server setup
- Custom command implementation  
- Storage backends
- Middleware and hooks
- Production deployment

### Testing with Official Redis Client

RedKit is fully tested with the official Redis Go client:

```go
import "github.com/redis/go-redis/v9"

// Connect to your RedKit server
rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379", // Your RedKit server
})

// Use all Redis commands
ctx := context.Background()

// String operations
rdb.Set(ctx, "key", "value", 0)
val := rdb.Get(ctx, "key").Val()

// Numeric operations  
rdb.Incr(ctx, "counter")
rdb.IncrBy(ctx, "score", 10)

// Multi-key operations
rdb.MSet(ctx, "key1", "val1", "key2", "val2")
vals := rdb.MGet(ctx, "key1", "key2").Val()

// Expiration
rdb.Expire(ctx, "session", 3600*time.Second)
ttl := rdb.TTL(ctx, "session").Val()

// All Redis clients work seamlessly!
```

##  License

This project is licensed under the terms specified in the [LICENSE](LICENSE) file.

---

<div align="center">
  <p>Built with ❤️ and Go</p>
  <p>⭐ Star this project if you find it useful!</p>
</div>
