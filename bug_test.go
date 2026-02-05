package redkit

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Helper function for string contains
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// =============================================================================
// BUG #1: QUIT command closes connection before sending response
// =============================================================================

func TestQuitCommandSendsResponseBeforeClose(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	address := fmt.Sprintf(":%d", port)
	server := NewServer(address)

	go func() {
		if err := server.Serve(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	// Connect directly with TCP to capture raw response
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Set read deadline to avoid hanging
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send QUIT command in RESP format
	_, err = conn.Write([]byte("*1\r\n$4\r\nQUIT\r\n"))
	if err != nil {
		t.Fatalf("Failed to send QUIT: %v", err)
	}

	// Try to read response - should get "+OK\r\n" before connection closes
	buf := make([]byte, 64)
	n, err := conn.Read(buf)

	// We should receive the OK response
	if err != nil {
		t.Errorf("BUG CONFIRMED: Failed to read response before connection closed: %v", err)
		return
	}

	response := string(buf[:n])
	if response != "+OK\r\n" {
		t.Errorf("Expected '+OK\\r\\n', got %q", response)
	} else {
		t.Logf("QUIT command correctly returned response: %q", response)
	}
}

// =============================================================================
// BUG #2: Panic in command handler doesn't return error response
// =============================================================================

func TestPanicInHandlerReturnsErrorResponse(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	address := fmt.Sprintf(":%d", port)
	server := NewServer(address)

	// Register a command that panics
	server.RegisterCommandFunc("PANIC_TEST", func(conn *Connection, cmd *Command) RedisValue {
		panic("intentional panic for testing")
	})

	go func() {
		if err := server.Serve(); err != nil {
			t.Logf("Server error: %v", err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	// Connect with TCP
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Send PANIC_TEST command
	_, err = conn.Write([]byte("*1\r\n$10\r\nPANIC_TEST\r\n"))
	if err != nil {
		t.Fatalf("Failed to send command: %v", err)
	}

	// Try to read response - should get an error response, not hang
	buf := make([]byte, 256)
	n, err := conn.Read(buf)

	if err != nil {
		t.Errorf("Connection failed/timed out instead of returning error response: %v", err)
		return
	}

	response := string(buf[:n])
	// Response should start with '-' (error)
	if len(response) == 0 {
		t.Errorf("Empty response after panic")
	} else if response[0] != '-' {
		t.Errorf("Expected error response starting with '-', got %q", response)
	} else {
		t.Logf("Panic correctly returned error response: %q", response)
	}
}

// =============================================================================
// BUG #3: RegisterCommand error message inconsistency
// =============================================================================

func TestRegisterCommandErrorMessages(t *testing.T) {
	server := NewServer(":0")

	t.Run("Empty name error message", func(t *testing.T) {
		handler := CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
			return RedisValue{Type: SimpleString, Str: "OK"}
		})

		err := server.RegisterCommand("", handler)
		if err == nil {
			t.Error("Expected error for empty command name")
			return
		}

		// Error message should mention "empty command name"
		if err.Error() != "empty command name" {
			t.Errorf("Unexpected error message for empty name: %v", err)
		}
	})

	t.Run("Nil handler error message", func(t *testing.T) {
		err := server.RegisterCommand("VALID_NAME", nil)
		if err == nil {
			t.Error("Expected error for nil handler")
			return
		}

		// Error message should mention nil handler, not "empty command name"
		errMsg := err.Error()
		if errMsg == "empty command name" {
			t.Errorf("BUG: Error message is '%s' but should mention nil handler", errMsg)
		} else if !contains(errMsg, "nil handler") {
			t.Errorf("Error message should mention 'nil handler', got: %s", errMsg)
		} else {
			t.Logf("Correct error message: %s", errMsg)
		}
	})

	t.Run("RegisterCommandFunc nil handler error message", func(t *testing.T) {
		err := server.RegisterCommandFunc("VALID_NAME2", nil)
		if err == nil {
			t.Error("Expected error for nil handler function")
			return
		}

		errMsg := err.Error()
		if errMsg == "empty command name" {
			t.Errorf("BUG: Error message is '%s' but should mention nil handler", errMsg)
		} else if !contains(errMsg, "nil handler") {
			t.Errorf("Error message should mention 'nil handler', got: %s", errMsg)
		} else {
			t.Logf("Correct error message: %s", errMsg)
		}
	})
}

// =============================================================================
// BUG #4: MiddlewareChain is not thread-safe
// =============================================================================

func TestMiddlewareChainThreadSafety(t *testing.T) {
	// This test checks if adding middlewares concurrently causes issues
	// Note: This is a design issue - middleware should typically be added before server starts
	// But if someone adds middleware at runtime, it should be safe

	chain := NewMiddlewareChain()

	var wg sync.WaitGroup
	numGoroutines := 100
	numAdds := 100

	// Try to add middlewares concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numAdds; j++ {
				// This may cause race condition if not thread-safe
				chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
					return next.Handle(conn, cmd)
				}))
			}
		}(i)
	}

	wg.Wait()

	// Check if all middlewares were added
	expectedCount := numGoroutines * numAdds
	actualCount := len(chain.middlewares)

	if actualCount != expectedCount {
		t.Errorf("BUG CONFIRMED: Expected %d middlewares, got %d (race condition detected)", expectedCount, actualCount)
	} else {
		t.Logf("All %d middlewares added correctly", actualCount)
	}
}

// =============================================================================
// BUG #5: IdleCheckFrequency not set in DefaultServerConfig
// =============================================================================

func TestDefaultServerConfigIdleCheckFrequency(t *testing.T) {
	config := DefaultServerConfig()

	// All timeout fields should be properly set
	if config.ReadTimeout == 0 {
		t.Error("ReadTimeout should not be 0")
	}
	if config.WriteTimeout == 0 {
		t.Error("WriteTimeout should not be 0")
	}
	if config.IdleTimeout == 0 {
		t.Error("IdleTimeout should not be 0")
	}
	if config.IdleCheckFrequency == 0 {
		t.Error("IdleCheckFrequency should not be 0")
	}

	t.Logf("Config values - ReadTimeout: %v, WriteTimeout: %v, IdleTimeout: %v, IdleCheckFrequency: %v",
		config.ReadTimeout, config.WriteTimeout, config.IdleTimeout, config.IdleCheckFrequency)
}

// =============================================================================
// Integration test: Server stability after panic
// =============================================================================

func TestServerStabilityAfterPanic(t *testing.T) {
	port, err := getFreePort()
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}

	address := fmt.Sprintf(":%d", port)
	server := NewServer(address)

	// Register panic command
	server.RegisterCommandFunc("CRASH", func(conn *Connection, cmd *Command) RedisValue {
		panic("crash!")
	})

	go func() {
		server.Serve()
	}()

	time.Sleep(100 * time.Millisecond)

	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(ctx)
	}()

	client := redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("localhost:%d", port),
		DialTimeout: 5 * time.Second,
	})
	defer client.Close()

	ctx := context.Background()

	// First, verify PING works
	_, err = client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Initial PING failed: %v", err)
	}

	// Trigger panic with custom command (this will fail because redis client doesn't know CRASH)
	// Use raw connection instead
	conn, err := net.Dial("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		t.Fatalf("Failed to connect for CRASH: %v", err)
	}

	conn.SetDeadline(time.Now().Add(3 * time.Second))
	conn.Write([]byte("*1\r\n$5\r\nCRASH\r\n"))

	buf := make([]byte, 256)
	_, readErr := conn.Read(buf)
	conn.Close()

	// Now test if server is still working with a new connection
	_, err = client.Ping(ctx).Result()
	if err != nil {
		t.Errorf("Server became unstable after panic - PING failed: %v (read error was: %v)", err, readErr)
	} else {
		t.Logf("Server remained stable after panic")
	}
}
