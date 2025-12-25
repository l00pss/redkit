package redkit

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Test helper functions

// getFreePort returns a free port for testing
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startRedisServer starts a Redis-compatible server with comprehensive command support
func startRedisServer(t *testing.T) (*Server, *redis.Client, func()) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	address := fmt.Sprintf(":%d", port)
	server := NewServer(address)

	// Setup in-memory storage with thread safety and expiration support
	storage := make(map[string]string)
	expiration := make(map[string]time.Time)
	mu := sync.RWMutex{}

	// Helper functions for expiration handling
	isExpired := func(key string) bool {
		if expTime, exists := expiration[key]; exists {
			return time.Now().After(expTime)
		}
		return false
	}

	cleanupExpired := func(key string) bool {
		if isExpired(key) {
			delete(storage, key)
			delete(expiration, key)
			return true
		}
		return false
	}

	// Register all Redis commands

	// PING command
	server.RegisterCommandFunc("PING", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) == 0 {
			return RedisValue{Type: SimpleString, Str: "PONG"}
		}
		if len(cmd.Args) == 1 {
			return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
		}
		return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'ping' command"}
	})

	// ECHO command
	server.RegisterCommandFunc("ECHO", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'echo' command"}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
	})

	// SET command
	server.RegisterCommandFunc("SET", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) < 2 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'set' command"}
		}
		mu.Lock()
		defer mu.Unlock()
		storage[cmd.Args[0]] = cmd.Args[1]
		delete(expiration, cmd.Args[0]) // Clear any existing expiration
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	// GET command
	server.RegisterCommandFunc("GET", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'get' command"}
		}
		mu.Lock()
		defer mu.Unlock()
		key := cmd.Args[0]
		if cleanupExpired(key) {
			return RedisValue{Type: Null}
		}
		value, exists := storage[key]
		if !exists {
			return RedisValue{Type: Null}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(value)}
	})

	// DEL command
	server.RegisterCommandFunc("DEL", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) < 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'del' command"}
		}
		mu.Lock()
		defer mu.Unlock()
		deleted := 0
		for _, key := range cmd.Args {
			if _, exists := storage[key]; exists {
				delete(storage, key)
				delete(expiration, key)
				deleted++
			}
		}
		return RedisValue{Type: Integer, Int: int64(deleted)}
	})

	// EXISTS command
	server.RegisterCommandFunc("EXISTS", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) < 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'exists' command"}
		}
		mu.Lock()
		defer mu.Unlock()
		count := 0
		for _, key := range cmd.Args {
			if !cleanupExpired(key) {
				if _, exists := storage[key]; exists {
					count++
				}
			}
		}
		return RedisValue{Type: Integer, Int: int64(count)}
	})

	// TTL command
	server.RegisterCommandFunc("TTL", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'ttl' command"}
		}
		mu.Lock()
		defer mu.Unlock()
		key := cmd.Args[0]
		if cleanupExpired(key) {
			return RedisValue{Type: Integer, Int: -2} // Key doesn't exist
		}
		if _, exists := storage[key]; !exists {
			return RedisValue{Type: Integer, Int: -2} // Key doesn't exist
		}
		if expTime, hasExpiry := expiration[key]; hasExpiry {
			ttl := int64(time.Until(expTime).Seconds())
			if ttl <= 0 {
				return RedisValue{Type: Integer, Int: -2}
			}
			return RedisValue{Type: Integer, Int: ttl}
		}
		return RedisValue{Type: Integer, Int: -1} // No expiry
	})

	// EXPIRE command
	server.RegisterCommandFunc("EXPIRE", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 2 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'expire' command"}
		}
		key := cmd.Args[0]
		seconds, err := strconv.Atoi(cmd.Args[1])
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR invalid expire time"}
		}

		mu.Lock()
		defer mu.Unlock()
		if cleanupExpired(key) {
			return RedisValue{Type: Integer, Int: 0} // Key doesn't exist
		}
		if _, exists := storage[key]; !exists {
			return RedisValue{Type: Integer, Int: 0} // Key doesn't exist
		}
		expiration[key] = time.Now().Add(time.Duration(seconds) * time.Second)
		return RedisValue{Type: Integer, Int: 1} // Expiration set
	})

	// INCR command
	server.RegisterCommandFunc("INCR", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'incr' command"}
		}
		key := cmd.Args[0]
		mu.Lock()
		defer mu.Unlock()

		if cleanupExpired(key) {
			storage[key] = "1"
			return RedisValue{Type: Integer, Int: 1}
		}

		value, exists := storage[key]
		if !exists {
			storage[key] = "1"
			return RedisValue{Type: Integer, Int: 1}
		}

		intVal, err := strconv.Atoi(value)
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR value is not an integer"}
		}

		intVal++
		storage[key] = strconv.Itoa(intVal)
		return RedisValue{Type: Integer, Int: int64(intVal)}
	})

	// INCRBY command
	server.RegisterCommandFunc("INCRBY", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 2 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'incrby' command"}
		}
		key := cmd.Args[0]
		increment, err := strconv.Atoi(cmd.Args[1])
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR invalid increment"}
		}

		mu.Lock()
		defer mu.Unlock()

		if cleanupExpired(key) {
			storage[key] = strconv.Itoa(increment)
			return RedisValue{Type: Integer, Int: int64(increment)}
		}

		value, exists := storage[key]
		if !exists {
			storage[key] = strconv.Itoa(increment)
			return RedisValue{Type: Integer, Int: int64(increment)}
		}

		intVal, err := strconv.Atoi(value)
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR value is not an integer"}
		}

		intVal += increment
		storage[key] = strconv.Itoa(intVal)
		return RedisValue{Type: Integer, Int: int64(intVal)}
	})

	// DECR command
	server.RegisterCommandFunc("DECR", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'decr' command"}
		}
		key := cmd.Args[0]
		mu.Lock()
		defer mu.Unlock()

		if cleanupExpired(key) {
			storage[key] = "-1"
			return RedisValue{Type: Integer, Int: -1}
		}

		value, exists := storage[key]
		if !exists {
			storage[key] = "-1"
			return RedisValue{Type: Integer, Int: -1}
		}

		intVal, err := strconv.Atoi(value)
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR value is not an integer"}
		}

		intVal--
		storage[key] = strconv.Itoa(intVal)
		return RedisValue{Type: Integer, Int: int64(intVal)}
	})

	// DECRBY command
	server.RegisterCommandFunc("DECRBY", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 2 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'decrby' command"}
		}
		key := cmd.Args[0]
		decrement, err := strconv.Atoi(cmd.Args[1])
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR invalid decrement"}
		}

		mu.Lock()
		defer mu.Unlock()

		if cleanupExpired(key) {
			result := -decrement
			storage[key] = strconv.Itoa(result)
			return RedisValue{Type: Integer, Int: int64(result)}
		}

		value, exists := storage[key]
		if !exists {
			result := -decrement
			storage[key] = strconv.Itoa(result)
			return RedisValue{Type: Integer, Int: int64(result)}
		}

		intVal, err := strconv.Atoi(value)
		if err != nil {
			return RedisValue{Type: ErrorReply, Str: "ERR value is not an integer"}
		}

		intVal -= decrement
		storage[key] = strconv.Itoa(intVal)
		return RedisValue{Type: Integer, Int: int64(intVal)}
	})

	// TYPE command
	server.RegisterCommandFunc("TYPE", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'type' command"}
		}
		key := cmd.Args[0]

		mu.Lock()
		defer mu.Unlock()

		if cleanupExpired(key) {
			return RedisValue{Type: SimpleString, Str: "none"}
		}

		if _, exists := storage[key]; exists {
			return RedisValue{Type: SimpleString, Str: "string"}
		}

		return RedisValue{Type: SimpleString, Str: "none"}
	})

	// KEYS command
	server.RegisterCommandFunc("KEYS", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'keys' command"}
		}
		pattern := cmd.Args[0]

		mu.Lock()
		defer mu.Unlock()

		var keys []RedisValue
		for key := range storage {
			if !cleanupExpired(key) {
				// Simple pattern matching - support * wildcard
				if pattern == "*" || key == pattern {
					keys = append(keys, RedisValue{Type: BulkString, Bulk: []byte(key)})
				}
			}
		}

		return RedisValue{Type: Array, Array: keys}
	})

	// SETNX command
	server.RegisterCommandFunc("SETNX", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 2 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'setnx' command"}
		}
		key, value := cmd.Args[0], cmd.Args[1]

		mu.Lock()
		defer mu.Unlock()

		if cleanupExpired(key) {
			storage[key] = value
			return RedisValue{Type: Integer, Int: 1}
		}

		if _, exists := storage[key]; exists {
			return RedisValue{Type: Integer, Int: 0} // Key already exists
		}

		storage[key] = value
		return RedisValue{Type: Integer, Int: 1} // Key was set
	})

	// MGET command
	server.RegisterCommandFunc("MGET", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) < 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'mget' command"}
		}

		mu.Lock()
		defer mu.Unlock()

		result := make([]RedisValue, len(cmd.Args))
		for i, key := range cmd.Args {
			if cleanupExpired(key) {
				result[i] = RedisValue{Type: Null}
			} else if value, exists := storage[key]; exists {
				result[i] = RedisValue{Type: BulkString, Bulk: []byte(value)}
			} else {
				result[i] = RedisValue{Type: Null}
			}
		}

		return RedisValue{Type: Array, Array: result}
	})

	// MSET command
	server.RegisterCommandFunc("MSET", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) < 2 || len(cmd.Args)%2 != 0 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'mset' command"}
		}

		mu.Lock()
		defer mu.Unlock()

		for i := 0; i < len(cmd.Args); i += 2 {
			key, value := cmd.Args[i], cmd.Args[i+1]
			storage[key] = value
			delete(expiration, key) // Clear any existing expiration
		}

		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	// FLUSHDB command
	server.RegisterCommandFunc("FLUSHDB", func(conn *Connection, cmd *Command) RedisValue {
		mu.Lock()
		defer mu.Unlock()
		storage = make(map[string]string)
		expiration = make(map[string]time.Time)
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	// FLUSHALL command
	server.RegisterCommandFunc("FLUSHALL", func(conn *Connection, cmd *Command) RedisValue {
		mu.Lock()
		defer mu.Unlock()
		storage = make(map[string]string)
		expiration = make(map[string]time.Time)
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	// Start server in goroutine
	go func() {
		if err := server.Serve(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("localhost:%d", port),
		Password:    "", // no password
		DB:          0,  // default DB
		DialTimeout: 5 * time.Second,
	})

	// Test connection
	ctx := context.Background()
	_, err = client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Failed to connect to Redis server: %v", err)
	}

	cleanup := func() {
		client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}

	return server, client, cleanup
}

// Basic command tests
func TestBasicRedisCommands(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("PING without message", func(t *testing.T) {
		result, err := client.Ping(ctx).Result()
		if err != nil {
			t.Errorf("PING failed: %v", err)
		}
		if result != "PONG" {
			t.Errorf("Expected PONG, got %s", result)
		}
	})

	t.Run("ECHO command", func(t *testing.T) {
		message := "Hello, Redis!"
		result, err := client.Echo(ctx, message).Result()
		if err != nil {
			t.Errorf("ECHO failed: %v", err)
		}
		if result != message {
			t.Errorf("Expected '%s', got '%s'", message, result)
		}
	})
}

// String operations tests
func TestStringOperations(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("SET and GET basic", func(t *testing.T) {
		key := "test:string"
		value := "test value"

		// SET
		err := client.Set(ctx, key, value, 0).Err()
		if err != nil {
			t.Errorf("SET failed: %v", err)
		}

		// GET
		result, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET failed: %v", err)
		}
		if result != value {
			t.Errorf("Expected '%s', got '%s'", value, result)
		}
	})

	t.Run("GET non-existent key", func(t *testing.T) {
		_, err := client.Get(ctx, "non-existent").Result()
		if err != redis.Nil {
			t.Errorf("Expected redis.Nil for non-existent key, got %v", err)
		}
	})

	t.Run("SET and GET multiple keys", func(t *testing.T) {
		testCases := map[string]string{
			"key1": "value1",
			"key2": "value2",
			"key3": "value3",
		}

		// Set all keys
		for key, value := range testCases {
			err := client.Set(ctx, key, value, 0).Err()
			if err != nil {
				t.Errorf("SET %s failed: %v", key, err)
			}
		}

		// Get and verify all keys
		for key, expectedValue := range testCases {
			result, err := client.Get(ctx, key).Result()
			if err != nil {
				t.Errorf("GET %s failed: %v", key, err)
			}
			if result != expectedValue {
				t.Errorf("Key %s: expected '%s', got '%s'", key, expectedValue, result)
			}
		}
	})

	t.Run("SET overwrites existing key", func(t *testing.T) {
		key := "overwrite:test"

		// Set initial value
		client.Set(ctx, key, "initial", 0)

		// Overwrite with new value
		err := client.Set(ctx, key, "overwritten", 0).Err()
		if err != nil {
			t.Errorf("SET overwrite failed: %v", err)
		}

		// Verify new value
		result, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET after overwrite failed: %v", err)
		}
		if result != "overwritten" {
			t.Errorf("Expected 'overwritten', got '%s'", result)
		}
	})
}

// Key management tests
func TestKeyManagement(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("EXISTS command", func(t *testing.T) {
		key := "exists:test"

		// Check non-existent key
		count, err := client.Exists(ctx, key).Result()
		if err != nil {
			t.Errorf("EXISTS failed: %v", err)
		}
		if count != 0 {
			t.Errorf("Expected 0 for non-existent key, got %d", count)
		}

		// Set key and check again
		client.Set(ctx, key, "value", 0)
		count, err = client.Exists(ctx, key).Result()
		if err != nil {
			t.Errorf("EXISTS failed: %v", err)
		}
		if count != 1 {
			t.Errorf("Expected 1 for existing key, got %d", count)
		}

		// Check multiple keys
		client.Set(ctx, "key1", "val1", 0)
		client.Set(ctx, "key2", "val2", 0)
		count, err = client.Exists(ctx, "key1", "key2", "non-existent").Result()
		if err != nil {
			t.Errorf("EXISTS multiple failed: %v", err)
		}
		if count != 2 {
			t.Errorf("Expected 2 existing keys, got %d", count)
		}
	})

	t.Run("DEL command", func(t *testing.T) {
		// Setup test keys
		keys := []string{"del:key1", "del:key2", "del:key3"}
		for _, key := range keys {
			client.Set(ctx, key, "value", 0)
		}

		// Delete single key
		deleted, err := client.Del(ctx, keys[0]).Result()
		if err != nil {
			t.Errorf("DEL failed: %v", err)
		}
		if deleted != 1 {
			t.Errorf("Expected 1 deleted key, got %d", deleted)
		}

		// Verify key is deleted
		_, err = client.Get(ctx, keys[0]).Result()
		if err != redis.Nil {
			t.Errorf("Key should be deleted")
		}

		// Delete multiple keys
		deleted, err = client.Del(ctx, keys[1], keys[2], "non-existent").Result()
		if err != nil {
			t.Errorf("DEL multiple failed: %v", err)
		}
		if deleted != 2 {
			t.Errorf("Expected 2 deleted keys, got %d", deleted)
		}
	})

	t.Run("TYPE command", func(t *testing.T) {
		key := "type:test"

		// Check type of non-existent key
		keyType, err := client.Type(ctx, key).Result()
		if err != nil {
			t.Errorf("TYPE failed: %v", err)
		}
		if keyType != "none" {
			t.Errorf("Expected 'none' for non-existent key, got '%s'", keyType)
		}

		// Set string and check type
		client.Set(ctx, key, "string value", 0)
		keyType, err = client.Type(ctx, key).Result()
		if err != nil {
			t.Errorf("TYPE failed: %v", err)
		}
		if keyType != "string" {
			t.Errorf("Expected 'string' type, got '%s'", keyType)
		}
	})

	t.Run("KEYS command", func(t *testing.T) {
		// Clear database first
		client.FlushDB(ctx)

		// Setup test data
		testKeys := []string{
			"keys:test:1",
			"keys:test:2",
			"keys:other:1",
			"different:key",
		}

		for _, key := range testKeys {
			client.Set(ctx, key, "value", 0)
		}

		// Get all keys
		keys, err := client.Keys(ctx, "*").Result()
		if err != nil {
			t.Errorf("KEYS * failed: %v", err)
		}
		if len(keys) != len(testKeys) {
			t.Errorf("Expected %d keys, got %d", len(testKeys), len(keys))
		}
	})
}

// Numeric operations tests
func TestNumericOperations(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("INCR operations", func(t *testing.T) {
		key := "incr:counter"

		// INCR on non-existent key
		result, err := client.Incr(ctx, key).Result()
		if err != nil {
			t.Errorf("INCR failed: %v", err)
		}
		if result != 1 {
			t.Errorf("Expected 1, got %d", result)
		}

		// INCR on existing key
		result, err = client.Incr(ctx, key).Result()
		if err != nil {
			t.Errorf("INCR failed: %v", err)
		}
		if result != 2 {
			t.Errorf("Expected 2, got %d", result)
		}

		// Verify final value
		value, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET failed: %v", err)
		}
		if value != "2" {
			t.Errorf("Expected '2', got '%s'", value)
		}
	})

	t.Run("INCRBY operations", func(t *testing.T) {
		key := "incrby:score"

		// INCRBY on non-existent key
		result, err := client.IncrBy(ctx, key, 10).Result()
		if err != nil {
			t.Errorf("INCRBY failed: %v", err)
		}
		if result != 10 {
			t.Errorf("Expected 10, got %d", result)
		}

		// INCRBY on existing key
		result, err = client.IncrBy(ctx, key, 25).Result()
		if err != nil {
			t.Errorf("INCRBY failed: %v", err)
		}
		if result != 35 {
			t.Errorf("Expected 35, got %d", result)
		}
	})

	t.Run("DECR operations", func(t *testing.T) {
		key := "decr:countdown"

		// Set initial value
		client.Set(ctx, key, "10", 0)

		// DECR operation
		result, err := client.Decr(ctx, key).Result()
		if err != nil {
			t.Errorf("DECR failed: %v", err)
		}
		if result != 9 {
			t.Errorf("Expected 9, got %d", result)
		}

		// DECR on non-existent key
		newKey := "decr:new"
		result, err = client.Decr(ctx, newKey).Result()
		if err != nil {
			t.Errorf("DECR on new key failed: %v", err)
		}
		if result != -1 {
			t.Errorf("Expected -1, got %d", result)
		}
	})

	t.Run("DECRBY operations", func(t *testing.T) {
		key := "decrby:points"

		// Set initial value
		client.Set(ctx, key, "100", 0)

		// DECRBY operation
		result, err := client.DecrBy(ctx, key, 30).Result()
		if err != nil {
			t.Errorf("DECRBY failed: %v", err)
		}
		if result != 70 {
			t.Errorf("Expected 70, got %d", result)
		}

		// DECRBY on non-existent key
		newKey := "decrby:new"
		result, err = client.DecrBy(ctx, newKey, 50).Result()
		if err != nil {
			t.Errorf("DECRBY on new key failed: %v", err)
		}
		if result != -50 {
			t.Errorf("Expected -50, got %d", result)
		}
	})

	t.Run("Mixed numeric operations", func(t *testing.T) {
		key := "mixed:calc"

		// Start from 0
		client.Set(ctx, key, "0", 0)

		// INCRBY 10
		result, _ := client.IncrBy(ctx, key, 10).Result()
		if result != 10 {
			t.Errorf("Expected 10, got %d", result)
		}

		// INCR by 1
		result, _ = client.Incr(ctx, key).Result()
		if result != 11 {
			t.Errorf("Expected 11, got %d", result)
		}

		// DECRBY 5
		result, _ = client.DecrBy(ctx, key, 5).Result()
		if result != 6 {
			t.Errorf("Expected 6, got %d", result)
		}

		// DECR by 1
		result, _ = client.Decr(ctx, key).Result()
		if result != 5 {
			t.Errorf("Expected 5, got %d", result)
		}
	})
}

// Expiration tests
func TestExpirationOperations(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("TTL on non-existent key", func(t *testing.T) {
		ttl, err := client.TTL(ctx, "non-existent").Result()
		if err != nil {
			t.Errorf("TTL failed: %v", err)
		}
		// Our server returns -2 for non-existent keys (in seconds)
		// go-redis converts this to nanoseconds, so -2 seconds = -2000000000 nanoseconds
		if ttl != -2*time.Nanosecond {
			t.Errorf("Expected -2ns for non-existent key, got %v", ttl)
		}
	})

	t.Run("TTL on persistent key", func(t *testing.T) {
		key := "persistent:key"
		client.Set(ctx, key, "value", 0)

		ttl, err := client.TTL(ctx, key).Result()
		if err != nil {
			t.Errorf("TTL failed: %v", err)
		}
		// Our server returns -1 for persistent keys (in seconds)
		// go-redis converts this to nanoseconds
		if ttl != -1*time.Nanosecond {
			t.Errorf("Expected -1ns for persistent key, got %v", ttl)
		}
	})

	t.Run("EXPIRE operations", func(t *testing.T) {
		key := "expire:test"

		// EXPIRE on non-existent key
		success, err := client.Expire(ctx, key, 60*time.Second).Result()
		if err != nil {
			t.Errorf("EXPIRE failed: %v", err)
		}
		if success {
			t.Errorf("Expected false for non-existent key")
		}

		// Set key and expire it
		client.Set(ctx, key, "value", 0)
		success, err = client.Expire(ctx, key, 30*time.Second).Result()
		if err != nil {
			t.Errorf("EXPIRE failed: %v", err)
		}
		if !success {
			t.Errorf("Expected true for successful expiration")
		}

		// Check TTL is set (should be between 1 and 30 seconds)
		ttl, err := client.TTL(ctx, key).Result()
		if err != nil {
			t.Errorf("TTL failed: %v", err)
		}
		ttlSeconds := int64(ttl.Seconds())
		if ttlSeconds <= 0 || ttlSeconds > 30 {
			t.Errorf("Expected TTL between 1-30 seconds, got %v (%d seconds)", ttl, ttlSeconds)
		}

		// Verify key still exists
		value, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET failed: %v", err)
		}
		if value != "value" {
			t.Errorf("Expected 'value', got '%s'", value)
		}
	})

	t.Run("Key expiration behavior", func(t *testing.T) {
		key := "expiring:key"

		// Set key with short expiration
		client.Set(ctx, key, "value", 0)
		client.Expire(ctx, key, 1*time.Second)

		// Verify key exists immediately
		exists, err := client.Exists(ctx, key).Result()
		if err != nil {
			t.Errorf("EXISTS failed: %v", err)
		}
		if exists != 1 {
			t.Errorf("Key should exist immediately after setting expiration")
		}

		// Wait for expiration
		time.Sleep(1500 * time.Millisecond)

		// Try to get the expired key
		_, err = client.Get(ctx, key).Result()
		if err != redis.Nil {
			t.Error("Expected redis.Nil for expired key")
		}

		// Verify key is cleaned up
		exists, err = client.Exists(ctx, key).Result()
		if err != nil {
			t.Errorf("EXISTS failed: %v", err)
		}
		if exists != 0 {
			t.Errorf("Expired key should not exist")
		}
	})
}

// Advanced operations tests
func TestAdvancedOperations(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("SETNX operations", func(t *testing.T) {
		key := "setnx:test"

		// SETNX on non-existent key
		success, err := client.SetNX(ctx, key, "value1", 0).Result()
		if err != nil {
			t.Errorf("SETNX failed: %v", err)
		}
		if !success {
			t.Errorf("Expected true for new key")
		}

		// Verify value was set
		value, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET failed: %v", err)
		}
		if value != "value1" {
			t.Errorf("Expected 'value1', got '%s'", value)
		}

		// SETNX on existing key
		success, err = client.SetNX(ctx, key, "value2", 0).Result()
		if err != nil {
			t.Errorf("SETNX failed: %v", err)
		}
		if success {
			t.Errorf("Expected false for existing key")
		}

		// Verify value was not changed
		value, err = client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET failed: %v", err)
		}
		if value != "value1" {
			t.Errorf("Value should not have changed, got '%s'", value)
		}
	})

	t.Run("MGET operations", func(t *testing.T) {
		// Clear DB and setup test data
		client.FlushDB(ctx)

		keys := []string{"mget:key1", "mget:key2", "mget:key3"}
		expectedValues := []string{"value1", "value2", "value3"}

		for i, key := range keys {
			client.Set(ctx, key, expectedValues[i], 0)
		}

		// MGET with existing keys
		values, err := client.MGet(ctx, keys...).Result()
		if err != nil {
			t.Errorf("MGET failed: %v", err)
		}
		if len(values) != 3 {
			t.Errorf("Expected 3 values, got %d", len(values))
		}

		for i, expected := range expectedValues {
			if values[i] == nil {
				t.Errorf("Expected value at index %d, got nil", i)
				continue
			}
			if actual := values[i].(string); actual != expected {
				t.Errorf("Expected '%s' at index %d, got '%s'", expected, i, actual)
			}
		}

		// MGET with mix of existing and non-existing keys
		mixedKeys := []string{keys[0], "non-existent", keys[2]}
		values, err = client.MGet(ctx, mixedKeys...).Result()
		if err != nil {
			t.Errorf("MGET mixed failed: %v", err)
		}
		if len(values) != 3 {
			t.Errorf("Expected 3 values, got %d", len(values))
		}

		if values[0] == nil || values[0].(string) != expectedValues[0] {
			t.Errorf("Expected '%s' at index 0, got %v", expectedValues[0], values[0])
		}
		if values[1] != nil {
			t.Errorf("Expected nil for non-existent key, got %v", values[1])
		}
		if values[2] == nil || values[2].(string) != expectedValues[2] {
			t.Errorf("Expected '%s' at index 2, got %v", expectedValues[2], values[2])
		}
	})

	t.Run("MSET operations", func(t *testing.T) {
		// Prepare key-value pairs
		pairs := []string{"mset:key1", "mvalue1", "mset:key2", "mvalue2", "mset:key3", "mvalue3"}

		// MSET multiple key-value pairs
		err := client.MSet(ctx, pairs).Err()
		if err != nil {
			t.Errorf("MSET failed: %v", err)
		}

		// Verify all keys were set correctly
		expectedPairs := map[string]string{
			"mset:key1": "mvalue1",
			"mset:key2": "mvalue2",
			"mset:key3": "mvalue3",
		}

		for key, expectedValue := range expectedPairs {
			value, err := client.Get(ctx, key).Result()
			if err != nil {
				t.Errorf("GET %s failed: %v", key, err)
			}
			if value != expectedValue {
				t.Errorf("Expected '%s' for key '%s', got '%s'", expectedValue, key, value)
			}
		}
	})
}

// Concurrency tests
func TestConcurrentOperations(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("Concurrent SET operations", func(t *testing.T) {
		const numGoroutines = 20
		const numOperations = 50

		var wg sync.WaitGroup
		errChan := make(chan error, numGoroutines*numOperations)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < numOperations; j++ {
					key := fmt.Sprintf("concurrent:set:%d:%d", goroutineID, j)
					value := fmt.Sprintf("value_%d_%d", goroutineID, j)
					if err := client.Set(ctx, key, value, 0).Err(); err != nil {
						errChan <- fmt.Errorf("SET failed for %s: %v", key, err)
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		for err := range errChan {
			t.Error(err)
		}
	})

	t.Run("Concurrent INCR operations", func(t *testing.T) {
		const numGoroutines = 20
		const incrementsPerGoroutine = 50

		key := "concurrent:counter"
		client.Set(ctx, key, "0", 0)

		var wg sync.WaitGroup
		errChan := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < incrementsPerGoroutine; j++ {
					if err := client.Incr(ctx, key).Err(); err != nil {
						errChan <- fmt.Errorf("INCR failed: %v", err)
						return
					}
				}
			}()
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		for err := range errChan {
			t.Error(err)
		}

		// Verify final count
		expectedCount := numGoroutines * incrementsPerGoroutine
		finalValue, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET final value failed: %v", err)
		}
		finalInt, err := strconv.Atoi(finalValue)
		if err != nil {
			t.Errorf("Parse final value failed: %v", err)
		}
		if finalInt != expectedCount {
			t.Errorf("Expected final count %d, got %d", expectedCount, finalInt)
		}
	})

	t.Run("Mixed concurrent operations", func(t *testing.T) {
		const numWorkers = 10
		const operationsPerWorker = 20

		var wg sync.WaitGroup
		errChan := make(chan error, numWorkers*operationsPerWorker)

		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				for j := 0; j < operationsPerWorker; j++ {
					key := fmt.Sprintf("mixed:worker:%d:op:%d", workerID, j)
					value := fmt.Sprintf("value_%d_%d", workerID, j)

					// SET
					if err := client.Set(ctx, key, value, 0).Err(); err != nil {
						errChan <- fmt.Errorf("SET failed: %v", err)
						continue
					}

					// GET
					if _, err := client.Get(ctx, key).Result(); err != nil {
						errChan <- fmt.Errorf("GET failed: %v", err)
						continue
					}

					// EXISTS
					if _, err := client.Exists(ctx, key).Result(); err != nil {
						errChan <- fmt.Errorf("EXISTS failed: %v", err)
						continue
					}

					// DEL
					if _, err := client.Del(ctx, key).Result(); err != nil {
						errChan <- fmt.Errorf("DEL failed: %v", err)
						continue
					}
				}
			}(i)
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		errorCount := 0
		for err := range errChan {
			t.Error(err)
			errorCount++
		}

		if errorCount > 0 {
			t.Errorf("Got %d errors during concurrent operations", errorCount)
		}
	})
}

// Database operations tests
func TestDatabaseOperations(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("FLUSHDB operation", func(t *testing.T) {
		// Setup some test data
		testKeys := []string{"flush:key1", "flush:key2", "flush:key3"}
		for _, key := range testKeys {
			client.Set(ctx, key, "value", 0)
		}

		// Verify data exists
		for _, key := range testKeys {
			exists, err := client.Exists(ctx, key).Result()
			if err != nil {
				t.Errorf("EXISTS failed: %v", err)
			}
			if exists != 1 {
				t.Errorf("Key %s should exist before FLUSHDB", key)
			}
		}

		// FLUSHDB
		err := client.FlushDB(ctx).Err()
		if err != nil {
			t.Errorf("FLUSHDB failed: %v", err)
		}

		// Verify all data is cleared
		for _, key := range testKeys {
			exists, err := client.Exists(ctx, key).Result()
			if err != nil {
				t.Errorf("EXISTS failed: %v", err)
			}
			if exists != 0 {
				t.Errorf("Key %s should not exist after FLUSHDB", key)
			}
		}
	})
}

// Error handling tests
func TestErrorHandling(t *testing.T) {
	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("INCR on non-numeric value", func(t *testing.T) {
		key := "error:non-numeric"
		client.Set(ctx, key, "not-a-number", 0)

		_, err := client.Incr(ctx, key).Result()
		if err == nil {
			t.Error("Expected error for INCR on non-numeric value")
		}
	})

	t.Run("INCRBY with invalid increment", func(t *testing.T) {
		key := "error:incrby"
		client.Set(ctx, key, "not-a-number", 0)

		_, err := client.IncrBy(ctx, key, 5).Result()
		if err == nil {
			t.Error("Expected error for INCRBY on non-numeric value")
		}
	})

	t.Run("EXPIRE with invalid time", func(t *testing.T) {
		// This would be handled by the Redis client before reaching our server
		// but we can test with zero or negative values
		key := "error:expire"
		client.Set(ctx, key, "value", 0)

		// Testing with zero should work (immediate expiration)
		success, err := client.Expire(ctx, key, 0).Result()
		if err != nil {
			t.Errorf("EXPIRE with 0 should work: %v", err)
		}
		if !success {
			t.Error("EXPIRE with 0 should return true")
		}
	})
}

// Performance benchmark tests
func BenchmarkRedisOperations(b *testing.B) {
	_, client, cleanup := startRedisServer(&testing.T{})
	defer cleanup()
	ctx := context.Background()

	b.Run("SET", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("bench:set:%d", i)
				value := fmt.Sprintf("value_%d", i)
				client.Set(ctx, key, value, 0)
				i++
			}
		})
	})

	b.Run("GET", func(b *testing.B) {
		// Setup data
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("bench:get:%d", i)
			value := fmt.Sprintf("value_%d", i)
			client.Set(ctx, key, value, 0)
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("bench:get:%d", i%10000)
				client.Get(ctx, key)
				i++
			}
		})
	})

	b.Run("INCR", func(b *testing.B) {
		client.Set(ctx, "bench:counter", "0", 0)

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				client.Incr(ctx, "bench:counter")
			}
		})
	})

	b.Run("MSET_MGET", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				// MSET
				pairs := []string{
					fmt.Sprintf("bench:mset:1:%d", i), "value1",
					fmt.Sprintf("bench:mset:2:%d", i), "value2",
					fmt.Sprintf("bench:mset:3:%d", i), "value3",
				}
				client.MSet(ctx, pairs)

				// MGET
				keys := []string{
					fmt.Sprintf("bench:mset:1:%d", i),
					fmt.Sprintf("bench:mset:2:%d", i),
					fmt.Sprintf("bench:mset:3:%d", i),
				}
				client.MGet(ctx, keys...)
				i++
			}
		})
	})

	b.Run("EXISTS", func(b *testing.B) {
		// Setup data
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("bench:exists:%d", i)
			client.Set(ctx, key, "value", 0)
		}

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("bench:exists:%d", i%1000)
				client.Exists(ctx, key)
				i++
			}
		})
	})

	b.Run("DEL", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				// Setup key
				key := fmt.Sprintf("bench:del:%d", i)
				client.Set(ctx, key, "value", 0)
				// Delete it
				client.Del(ctx, key)
				i++
			}
		})
	})
}

// Stress tests
func TestStressOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress tests in short mode")
	}

	_, client, cleanup := startRedisServer(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("High volume operations", func(t *testing.T) {
		const numKeys = 10000
		keys := make([]string, numKeys)

		// Batch SET operations
		for i := 0; i < numKeys; i++ {
			key := fmt.Sprintf("stress:key:%d", i)
			value := fmt.Sprintf("stress_value_%d", i)
			keys[i] = key

			err := client.Set(ctx, key, value, 0).Err()
			if err != nil {
				t.Errorf("SET failed for key %s: %v", key, err)
			}
		}

		// Verify with MGET in batches
		batchSize := 100
		for i := 0; i < len(keys); i += batchSize {
			end := i + batchSize
			if end > len(keys) {
				end = len(keys)
			}

			batch := keys[i:end]
			values, err := client.MGet(ctx, batch...).Result()
			if err != nil {
				t.Errorf("MGET failed for batch %d-%d: %v", i, end, err)
				continue
			}

			if len(values) != len(batch) {
				t.Errorf("Expected %d values, got %d", len(batch), len(values))
				continue
			}

			for j, value := range values {
				if value == nil {
					t.Errorf("Got nil value for key %s", batch[j])
					continue
				}

				expectedValue := fmt.Sprintf("stress_value_%d", i+j)
				if actual := value.(string); actual != expectedValue {
					t.Errorf("Value mismatch for key %s: expected %s, got %s",
						batch[j], expectedValue, actual)
				}
			}
		}

		// Cleanup with batch DEL
		for i := 0; i < len(keys); i += batchSize {
			end := i + batchSize
			if end > len(keys) {
				end = len(keys)
			}

			batch := keys[i:end]
			deleted, err := client.Del(ctx, batch...).Result()
			if err != nil {
				t.Errorf("DEL failed for batch: %v", err)
				continue
			}

			if int(deleted) != len(batch) {
				t.Errorf("Expected %d deleted keys, got %d", len(batch), deleted)
			}
		}
	})

	t.Run("Rapid numeric operations", func(t *testing.T) {
		const numOperations = 10000
		key := "stress:counter"

		// Initialize counter
		client.Set(ctx, key, "0", 0)

		// Rapid increments
		for i := 0; i < numOperations; i++ {
			result, err := client.Incr(ctx, key).Result()
			if err != nil {
				t.Errorf("INCR failed at iteration %d: %v", i, err)
				break
			}
			if result != int64(i+1) {
				t.Errorf("Expected %d, got %d at iteration %d", i+1, result, i)
			}
		}

		// Verify final value
		value, err := client.Get(ctx, key).Result()
		if err != nil {
			t.Errorf("GET final value failed: %v", err)
		} else if value != fmt.Sprintf("%d", numOperations) {
			t.Errorf("Expected final value %d, got %s", numOperations, value)
		}

		// Rapid decrements back to zero
		for i := numOperations; i > 0; i-- {
			result, err := client.Decr(ctx, key).Result()
			if err != nil {
				t.Errorf("DECR failed at iteration %d: %v", i, err)
				break
			}
			if result != int64(i-1) {
				t.Errorf("Expected %d, got %d at iteration %d", i-1, result, i)
			}
		}
	})
}
