package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/l00pss/redkit"
)

func main() {
	// Create a new RedKit server
	server := redkit.NewServer(":6379")

	// Register custom commands
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

	// Register a simple SET/GET simulation with thread-safe storage
	storage := make(map[string]string)
	var storageMu sync.RWMutex

	server.RegisterCommandFunc(string(redkit.SET), func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
		if len(cmd.Args) != 2 {
			return redkit.RedisValue{
				Type: redkit.ErrorReply,
				Str:  "ERR wrong number of arguments for 'set' command",
			}
		}
		storageMu.Lock()
		storage[cmd.Args[0]] = cmd.Args[1]
		storageMu.Unlock()
		return redkit.RedisValue{
			Type: redkit.SimpleString,
			Str:  "OK",
		}
	})

	server.RegisterCommandFunc(string(redkit.GET), func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
		if len(cmd.Args) != 1 {
			return redkit.RedisValue{
				Type: redkit.ErrorReply,
				Str:  "ERR wrong number of arguments for 'get' command",
			}
		}
		storageMu.RLock()
		value, exists := storage[cmd.Args[0]]
		storageMu.RUnlock()
		if !exists {
			return redkit.RedisValue{Type: redkit.Null}
		}
		return redkit.RedisValue{
			Type: redkit.BulkString,
			Bulk: []byte(value),
		}
	})

	// Register CONFIG command (for redis-benchmark compatibility)
	server.RegisterCommandFunc("CONFIG", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
		// Simple CONFIG GET implementation
		if len(cmd.Args) >= 2 && cmd.Args[0] == "GET" {
			return redkit.RedisValue{
				Type:  redkit.Array,
				Array: []redkit.RedisValue{},
			}
		}
		return redkit.RedisValue{
			Type: redkit.SimpleString,
			Str:  "OK",
		}
	})

	// Handle graceful shutdown
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

	fmt.Println("Starting RedKit server on :6379...")
	fmt.Println("You can test it with redis-cli or any Redis client")
	fmt.Println("Try commands like: PING, HELLO, HELLO world, SET key value, GET key")

	if err := server.Serve(); err != nil {
		log.Fatal(err)
	}
}
