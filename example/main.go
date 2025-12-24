package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
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

	// Register a simple SET/GET simulation
	storage := make(map[string]string)

	server.RegisterCommandFunc("SET", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
		if len(cmd.Args) != 2 {
			return redkit.RedisValue{
				Type: redkit.ErrorReply,
				Str:  "ERR wrong number of arguments for 'set' command",
			}
		}
		storage[cmd.Args[0]] = cmd.Args[1]
		return redkit.RedisValue{
			Type: redkit.SimpleString,
			Str:  "OK",
		}
	})

	server.RegisterCommandFunc("GET", func(conn *redkit.Connection, cmd *redkit.Command) redkit.RedisValue {
		if len(cmd.Args) != 1 {
			return redkit.RedisValue{
				Type: redkit.ErrorReply,
				Str:  "ERR wrong number of arguments for 'get' command",
			}
		}
		value, exists := storage[cmd.Args[0]]
		if !exists {
			return redkit.RedisValue{Type: redkit.Null}
		}
		return redkit.RedisValue{
			Type: redkit.BulkString,
			Bulk: []byte(value),
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

	// Start the server
	fmt.Println("Starting RedKit server on :6379...")
	fmt.Println("You can test it with redis-cli or any Redis client")
	fmt.Println("Try commands like: PING, HELLO, HELLO world, SET key value, GET key")

	if err := server.Serve(); err != nil {
		log.Fatal(err)
	}
}
