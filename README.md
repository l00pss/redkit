<br>
<div align="center">
  <img src="logo.png" alt="RedKit Logo" width="600"/>
  <br><br>
  
  [![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://golang.org/)
  [![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
  [![Redis Compatible](https://img.shields.io/badge/Redis-Compatible-red.svg)](https://redis.io/)
</div>

A lightweight, high-performance Redis-compatible server framework for Go

##  Features

-  **Full Redis RESP Protocol** - Works with any Redis client (redis-cli, go-redis, jedis)
-  **Middleware Chain Support** - Logging, auth, timing, and custom interceptors
-  **Easy to extend** - Simple command registration API
-  **High performance** - Built for speed with Go's concurrency
- ️ **TLS Support** - Secure connections
-  **Connection management** - State tracking and idle timeouts
-  **Comprehensive tests** - Tested with official Redis clients

##  Quick Start

```bash
go get github.com/l00pss/redkit
```

### Basic Server

```go
package main

import "github.com/l00pss/redkit"

func main() {
    server := redkit.NewServer(":6379")
    
    // Register custom command
    server.RegisterCommandFunc("HELLO", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
        return redkit.RedisValue{Type: redkit.SimpleString, Str: "Hello from RedKit!"}
    })
    
    server.Serve()
}
```

### With Middleware

```go
server := redkit.NewServer(":6379")

// Add logging middleware
server.UseFunc(func(conn *redkit.Connection, cmd *redkit.Command, next redkit.CommandHandler) redkit.RedisValue {
    log.Printf("Command: %s", cmd.Name)
    result := next.Handle(conn, cmd)
    log.Printf("Result: %v", result.Type)
    return result
})

// Add auth middleware
server.UseFunc(func(conn *redkit.Connection, cmd *redkit.Command, next redkit.CommandHandler) redkit.RedisValue {
    if !isAuthenticated(conn) && cmd.Name != "AUTH" {
        return redkit.RedisValue{Type: redkit.ErrorReply, Str: "NOAUTH Authentication required"}
    }
    return next.Handle(conn, cmd)
})

server.Serve()
```

##  Testing

```bash
redis-cli -h localhost -p 6379

127.0.0.1:6379> PING
PONG

127.0.0.1:6379> SET key value
OK

127.0.0.1:6379> GET key
"value"
```

##  Built-in Commands

- `PING`, `ECHO`, `QUIT` - Connection commands
- `SET`, `GET`, `MSET`, `MGET`, `SETNX` - String operations
- `DEL`, `EXISTS`, `TYPE`, `KEYS` - Key management
- `INCR`, `DECR`, `INCRBY`, `DECRBY` - Numeric operations
- `EXPIRE`, `TTL` - Expiration management
- `FLUSHDB`, `FLUSHALL` - Database operations

##  Configuration

```go
// Simple usage
server := redkit.NewServer(":6379")

// Advanced configuration
config := redkit.DefaultServerConfig()
config.Address = ":6379"
config.ReadTimeout = 30 * time.Second
config.WriteTimeout = 30 * time.Second
config.IdleTimeout = 120 * time.Second
config.MaxConnections = 1000
config.TLSConfig = &tls.Config{...}
config.ConnStateHook = func(conn net.Conn, state redkit.ConnState) {
    log.Printf("Connection %s: %v", conn.RemoteAddr(), state)
}

server := redkit.NewServerWithConfig(config)
```

##  Development

```bash
# Run tests
go test -v

# Run with race detector
go test -race -v

# Run benchmarks
go test -bench=.
```

##  License

MIT License - see [LICENSE](LICENSE) file for details.

---

<div align="center">
  <p>Built with ❤️ and Go</p>
  <p>⭐ Star this project if you find it useful!</p>
</div>
