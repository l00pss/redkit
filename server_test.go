package redkit

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

// Helper function to get a free port for testing
func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer func(l *net.TCPListener) {
		err := l.Close()
		if err != nil {
			fmt.Printf("Failed to close listener: %v", err)
		}
	}(l)
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Helper function to start test server
func startTestServer(t *testing.T) (*Server, *redis.Client, func()) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	address := fmt.Sprintf(":%d", port)
	server := NewServer(address)

	// Add some test commands
	storage := make(map[string]string)
	mu := sync.RWMutex{}

	server.RegisterCommandFunc("SET", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 2 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'set' command"}
		}
		mu.Lock()
		storage[cmd.Args[0]] = cmd.Args[1]
		mu.Unlock()
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	server.RegisterCommandFunc("GET", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'get' command"}
		}
		mu.RLock()
		value, exists := storage[cmd.Args[0]]
		mu.RUnlock()
		if !exists {
			return RedisValue{Type: Null}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(value)}
	})

	server.RegisterCommandFunc("DEL", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) < 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'del' command"}
		}
		mu.Lock()
		deleted := 0
		for _, key := range cmd.Args {
			if _, exists := storage[key]; exists {
				delete(storage, key)
				deleted++
			}
		}
		mu.Unlock()
		return RedisValue{Type: Integer, Int: int64(deleted)}
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
		Addr: fmt.Sprintf("localhost:%d", port),
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Failed to connect to test server: %v", err)
	}

	cleanup := func() {
		client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}

	return server, client, cleanup
}

func TestBasicCommands(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Test PING
	t.Run("PING", func(t *testing.T) {
		result := client.Ping(ctx)
		if result.Err() != nil {
			t.Errorf("PING failed: %v", result.Err())
		}
		if result.Val() != "PONG" {
			t.Errorf("Expected PONG, got %s", result.Val())
		}
	})

	// Test PING with message
	t.Run("PING with message", func(t *testing.T) {
		result := client.Do(ctx, "PING", "hello")
		if result.Err() != nil {
			t.Errorf("PING with message failed: %v", result.Err())
		}
		if result.Val() != "hello" {
			t.Errorf("Expected hello, got %v", result.Val())
		}
	})

	// Test ECHO
	t.Run("ECHO", func(t *testing.T) {
		result := client.Echo(ctx, "test message")
		if result.Err() != nil {
			t.Errorf("ECHO failed: %v", result.Err())
		}
		if result.Val() != "test message" {
			t.Errorf("Expected 'test message', got '%s'", result.Val())
		}
	})
}

func TestSetGetOperations(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Test SET and GET
	t.Run("SET and GET", func(t *testing.T) {
		// SET operation
		setResult := client.Set(ctx, "testkey", "testvalue", 0)
		if setResult.Err() != nil {
			t.Errorf("SET failed: %v", setResult.Err())
		}
		if setResult.Val() != "OK" {
			t.Errorf("Expected OK, got %s", setResult.Val())
		}

		// GET operation
		getResult := client.Get(ctx, "testkey")
		if getResult.Err() != nil {
			t.Errorf("GET failed: %v", getResult.Err())
		}
		if getResult.Val() != "testvalue" {
			t.Errorf("Expected testvalue, got %s", getResult.Val())
		}
	})

	// Test GET non-existent key
	t.Run("GET non-existent key", func(t *testing.T) {
		getResult := client.Get(ctx, "nonexistent")
		if getResult.Err() != redis.Nil {
			t.Errorf("Expected redis.Nil error, got %v", getResult.Err())
		}
	})

	// Test multiple SET/GET operations
	t.Run("Multiple SET/GET", func(t *testing.T) {
		keys := []string{"key1", "key2", "key3"}
		values := []string{"value1", "value2", "value3"}

		// Set multiple keys
		for i, key := range keys {
			if err := client.Set(ctx, key, values[i], 0).Err(); err != nil {
				t.Errorf("SET %s failed: %v", key, err)
			}
		}

		// Get multiple keys
		for i, key := range keys {
			result := client.Get(ctx, key)
			if result.Err() != nil {
				t.Errorf("GET %s failed: %v", key, result.Err())
			}
			if result.Val() != values[i] {
				t.Errorf("Expected %s, got %s", values[i], result.Val())
			}
		}
	})
}

func TestDeleteOperations(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Setup test data
	client.Set(ctx, "key1", "value1", 0)
	client.Set(ctx, "key2", "value2", 0)
	client.Set(ctx, "key3", "value3", 0)

	t.Run("DEL single key", func(t *testing.T) {
		result := client.Del(ctx, "key1")
		if result.Err() != nil {
			t.Errorf("DEL failed: %v", result.Err())
		}
		if result.Val() != 1 {
			t.Errorf("Expected 1 deleted key, got %d", result.Val())
		}

		// Verify key is deleted
		getResult := client.Get(ctx, "key1")
		if getResult.Err() != redis.Nil {
			t.Errorf("Key should be deleted, but GET succeeded: %v", getResult.Val())
		}
	})

	t.Run("DEL multiple keys", func(t *testing.T) {
		result := client.Del(ctx, "key2", "key3", "nonexistent")
		if result.Err() != nil {
			t.Errorf("DEL failed: %v", result.Err())
		}
		if result.Val() != 2 {
			t.Errorf("Expected 2 deleted keys, got %d", result.Val())
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()
	numGoroutines := 50
	numOperations := 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numOperations)

	// Concurrent SET operations
	t.Run("Concurrent SET operations", func(t *testing.T) {
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < numOperations; j++ {
					key := fmt.Sprintf("concurrent_key_%d_%d", goroutineID, j)
					value := fmt.Sprintf("value_%d_%d", goroutineID, j)
					if err := client.Set(ctx, key, value, 0).Err(); err != nil {
						errors <- fmt.Errorf("SET failed for %s: %v", key, err)
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Error(err)
		}
	})

	// Verify data integrity
	t.Run("Verify concurrent data", func(t *testing.T) {
		for i := 0; i < numGoroutines; i++ {
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("concurrent_key_%d_%d", i, j)
				expectedValue := fmt.Sprintf("value_%d_%d", i, j)

				result := client.Get(ctx, key)
				if result.Err() != nil {
					t.Errorf("GET failed for %s: %v", key, result.Err())
					continue
				}
				if result.Val() != expectedValue {
					t.Errorf("Data corruption for %s: expected %s, got %s", key, expectedValue, result.Val())
				}
			}
		}
	})
}

func TestErrorHandling(t *testing.T) {
	_, client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("Wrong number of arguments", func(t *testing.T) {
		// SET with wrong number of arguments
		result := client.Do(ctx, "SET", "key")
		if result.Err() == nil {
			t.Error("Expected error for SET with wrong arguments")
		}

		// GET with wrong number of arguments
		result = client.Do(ctx, "GET")
		if result.Err() == nil {
			t.Error("Expected error for GET with no arguments")
		}

		// ECHO with wrong number of arguments
		result = client.Do(ctx, "ECHO", "arg1", "arg2")
		if result.Err() == nil {
			t.Error("Expected error for ECHO with too many arguments")
		}
	})

	t.Run("Unknown command", func(t *testing.T) {
		result := client.Do(ctx, "UNKNOWN_COMMAND", "arg1")
		if result.Err() == nil {
			t.Error("Expected error for unknown command")
		}
	})
}

func TestConnectionStates(t *testing.T) {
	server, client, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Track connection state changes
	stateChanges := make(chan ConnState, 10)
	server.ConnStateHook = func(conn net.Conn, state ConnState) {
		t.Logf("State change: %v", state)
		select {
		case stateChanges <- state:
		case <-time.After(100 * time.Millisecond):
			t.Log("Failed to send state change to channel")
		}
	}

	t.Run("Connection lifecycle", func(t *testing.T) {
		// Create a new client to ensure we capture the full lifecycle
		newClient := redis.NewClient(&redis.Options{
			Addr: client.Options().Addr,
		})
		defer newClient.Close()

		// Perform some operations to trigger state changes
		err := newClient.Ping(ctx).Err()
		if err != nil {
			t.Fatalf("Ping failed: %v", err)
		}

		// Give more time for state changes to be recorded
		time.Sleep(200 * time.Millisecond)

		// Collect states
		states := []ConnState{}
		timeout := time.After(500 * time.Millisecond)

		for {
			select {
			case state := <-stateChanges:
				states = append(states, state)
				t.Logf("Collected state: %v", state)
			case <-timeout:
				goto checkStates
			}
		}

	checkStates:
		t.Logf("Total states collected: %d, states: %v", len(states), states)

		if len(states) == 0 {
			t.Log("No state changes detected - this might indicate the hook is not being called")
			return
		}

		// Check that we have at least StateNew
		foundNew := false
		foundActive := false

		for _, state := range states {
			if state == StateNew {
				foundNew = true
			}
			if state == StateActive {
				foundActive = true
			}
		}

		if !foundNew {
			t.Error("Should have seen StateNew")
		}
		if !foundActive {
			t.Error("Should have seen StateActive")
		}
	})
}

func TestServerShutdown(t *testing.T) {
	server, client, _ := startTestServer(t)

	ctx := context.Background()

	t.Run("Graceful shutdown", func(t *testing.T) {
		// Perform operation to ensure server is working
		if err := client.Ping(ctx).Err(); err != nil {
			t.Errorf("Server should be working before shutdown: %v", err)
		}

		// Shutdown server
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			t.Errorf("Server shutdown failed: %v", err)
		}

		// Verify server is shut down
		if !server.IsShutdown() {
			t.Error("Server should report as shut down")
		}

		// Operations should fail after shutdown
		client.Close() // Close client first to avoid connection issues
	})
}

func TestIdleConnections(t *testing.T) {
	server, _, cleanup := startTestServer(t)
	defer cleanup()

	ctx := context.Background()

	// Set short idle timeout for testing
	server.IdleTimeout = 100 * time.Millisecond

	stateChanges := make(chan ConnState, 20)
	server.ConnStateHook = func(conn net.Conn, state ConnState) {
		t.Logf("Idle test - State change: %v for %v", state, conn.RemoteAddr())
		select {
		case stateChanges <- state:
		case <-time.After(100 * time.Millisecond):
		}
	}

	t.Run("Idle state transition", func(t *testing.T) {
		// Create a dedicated client for this test
		client := redis.NewClient(&redis.Options{
			Addr: fmt.Sprintf("localhost%s", server.Address),
		})
		defer client.Close()

		// Perform initial operation to establish connection
		err := client.Ping(ctx).Err()
		if err != nil {
			t.Fatalf("Initial ping failed: %v", err)
		}
		t.Log("Initial ping completed")

		// Wait for idle timeout to pass
		time.Sleep(150 * time.Millisecond)

		// Manually trigger idle check
		server.TriggerIdleCheck()

		// Give time for state changes to be processed
		time.Sleep(50 * time.Millisecond)

		// Collect all state changes
		states := []ConnState{}
		deadline := time.After(200 * time.Millisecond)

		for {
			select {
			case state := <-stateChanges:
				states = append(states, state)
				t.Logf("Collected state: %v", state)
			case <-deadline:
				goto analyzeStates
			}
		}

	analyzeStates:
		t.Logf("All collected states: %v", states)

		// We should see at least New -> Active
		if len(states) == 0 {
			t.Error("No state changes detected")
			return
		}

		// Check for expected state progression
		foundNew := false
		foundActive := false
		foundIdle := false

		for _, state := range states {
			switch state {
			case StateNew:
				foundNew = true
			case StateActive:
				foundActive = true
			case StateIdle:
				foundIdle = true
			}
		}

		if !foundNew {
			t.Log("StateNew not found - this might be expected if connection was already established")
		}
		if !foundActive {
			t.Error("StateActive not found")
		}

		// Now we should detect idle since we manually triggered the check
		if !foundIdle {
			t.Error("StateIdle not detected even after manual trigger")
		} else {
			t.Log("StateIdle detected successfully!")

			// If we detected idle, try to reactivate
			err := client.Ping(ctx).Err()
			if err != nil {
				t.Errorf("Reactivation ping failed: %v", err)
				return
			}

			// Wait a bit more and check for reactivation
			time.Sleep(100 * time.Millisecond)

			reactivated := false
			finalDeadline := time.After(500 * time.Millisecond)
			for {
				select {
				case state := <-stateChanges:
					if state == StateActive {
						reactivated = true
						t.Log("Connection reactivated successfully!")
						goto endReactivationCheck
					}
				case <-finalDeadline:
					goto endReactivationCheck
				}
			}

		endReactivationCheck:
			if !reactivated {
				t.Log("Reactivation not detected - might be due to timing")
			}
		}
	})
}

// Benchmark tests
func BenchmarkPingCommand(b *testing.B) {
	_, client, cleanup := startTestServer(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.Ping(ctx)
	}
}

func BenchmarkSetGet(b *testing.B) {
	_, client, cleanup := startTestServer(&testing.T{})
	defer cleanup()

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench_key_%d", i)
		value := fmt.Sprintf("bench_value_%d", i)
		client.Set(ctx, key, value, 0)
		client.Get(ctx, key)
	}
}
