package redkit

import (
	"fmt"
	"strings"
	"testing"
)

// TestMiddlewareChain tests that middlewares are called in correct order
func TestMiddlewareChain(t *testing.T) {
	var executionOrder []string

	// Create middleware chain
	chain := NewMiddlewareChain()

	// Add first middleware
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		executionOrder = append(executionOrder, "MW1-before")
		result := next.Handle(conn, cmd) // Call next middleware or handler
		executionOrder = append(executionOrder, "MW1-after")
		return result
	}))

	// Add second middleware
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		executionOrder = append(executionOrder, "MW2-before")
		result := next.Handle(conn, cmd) // Call next middleware or handler
		executionOrder = append(executionOrder, "MW2-after")
		return result
	}))

	// Add third middleware
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		executionOrder = append(executionOrder, "MW3-before")
		result := next.Handle(conn, cmd) // Call final handler
		executionOrder = append(executionOrder, "MW3-after")
		return result
	}))

	// Final command handler
	handler := CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
		executionOrder = append(executionOrder, "HANDLER")
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	// Execute the chain
	cmd := &Command{Name: "TEST"}
	result := chain.Execute(nil, cmd, handler)

	// Verify execution order
	expected := []string{
		"MW1-before",
		"MW2-before",
		"MW3-before",
		"HANDLER",
		"MW3-after",
		"MW2-after",
		"MW1-after",
	}

	if len(executionOrder) != len(expected) {
		t.Fatalf("Expected %d execution steps, got %d", len(expected), len(executionOrder))
	}

	for i, step := range expected {
		if executionOrder[i] != step {
			t.Errorf("Step %d: expected %s, got %s", i, step, executionOrder[i])
		}
	}

	// Verify result
	if result.Type != SimpleString || result.Str != "OK" {
		t.Errorf("Expected OK result, got %v", result)
	}

	t.Logf("Execution order: %s", strings.Join(executionOrder, " -> "))
}

// TestMiddlewareCanModifyRequest tests that middleware can modify the command
func TestMiddlewareCanModifyRequest(t *testing.T) {
	chain := NewMiddlewareChain()

	// Middleware that modifies the command
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		// Add a prefix to all arguments
		modifiedCmd := &Command{
			Name: cmd.Name,
			Args: make([]string, len(cmd.Args)),
			Raw:  cmd.Raw,
		}
		for i, arg := range cmd.Args {
			modifiedCmd.Args[i] = "modified-" + arg
		}
		return next.Handle(conn, modifiedCmd)
	}))

	// Handler that checks the modified args
	handler := CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) == 0 {
			return RedisValue{Type: ErrorReply, Str: "No args"}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
	})

	cmd := &Command{Name: "TEST", Args: []string{"hello"}}
	result := chain.Execute(nil, cmd, handler)

	if result.Type != BulkString {
		t.Fatalf("Expected BulkString, got %v", result.Type)
	}

	if string(result.Bulk) != "modified-hello" {
		t.Errorf("Expected 'modified-hello', got '%s'", string(result.Bulk))
	}
}

// TestMiddlewareCanModifyResponse tests that middleware can modify the response
func TestMiddlewareCanModifyResponse(t *testing.T) {
	chain := NewMiddlewareChain()

	// Middleware that wraps the response
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		result := next.Handle(conn, cmd)

		// Wrap the response in an array
		return RedisValue{
			Type: Array,
			Array: []RedisValue{
				{Type: SimpleString, Str: "wrapped"},
				result,
			},
		}
	}))

	// Handler returns a simple string
	handler := CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
		return RedisValue{Type: SimpleString, Str: "original"}
	})

	cmd := &Command{Name: "TEST"}
	result := chain.Execute(nil, cmd, handler)

	if result.Type != Array {
		t.Fatalf("Expected Array, got %v", result.Type)
	}

	if len(result.Array) != 2 {
		t.Fatalf("Expected 2 elements, got %d", len(result.Array))
	}

	if result.Array[0].Str != "wrapped" {
		t.Errorf("Expected 'wrapped', got '%s'", result.Array[0].Str)
	}

	if result.Array[1].Str != "original" {
		t.Errorf("Expected 'original', got '%s'", result.Array[1].Str)
	}
}

// TestMiddlewareCanShortCircuit tests that middleware can stop the chain
func TestMiddlewareCanShortCircuit(t *testing.T) {
	chain := NewMiddlewareChain()
	var handlerCalled bool

	// Auth middleware that blocks the request
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		// Simulate auth failure - don't call next
		return RedisValue{Type: ErrorReply, Str: "NOAUTH Authentication required"}
	}))

	// This middleware should never be called
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		t.Error("Second middleware should not be called")
		return next.Handle(conn, cmd)
	}))

	// Handler should never be called
	handler := CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
		handlerCalled = true
		return RedisValue{Type: SimpleString, Str: "OK"}
	})

	cmd := &Command{Name: "GET", Args: []string{"key"}}
	result := chain.Execute(nil, cmd, handler)

	if handlerCalled {
		t.Error("Handler should not have been called")
	}

	if result.Type != ErrorReply {
		t.Errorf("Expected ErrorReply, got %v", result.Type)
	}

	if result.Str != "NOAUTH Authentication required" {
		t.Errorf("Expected auth error, got '%s'", result.Str)
	}
}

// TestMiddlewareChainExample demonstrates a real-world usage
func TestMiddlewareChainExample(t *testing.T) {
	var log []string

	chain := NewMiddlewareChain()

	// Logging middleware
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		log = append(log, fmt.Sprintf("LOG: Command=%s", cmd.Name))
		result := next.Handle(conn, cmd)
		log = append(log, fmt.Sprintf("LOG: Result=%v", result.Type))
		return result
	}))

	// Metrics middleware
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		log = append(log, "METRICS: Recording command")
		result := next.Handle(conn, cmd)
		log = append(log, "METRICS: Command completed")
		return result
	}))

	// Timing middleware
	chain.Add(MiddlewareFunc(func(conn *Connection, cmd *Command, next CommandHandler) RedisValue {
		log = append(log, "TIMING: Start")
		result := next.Handle(conn, cmd)
		log = append(log, "TIMING: End")
		return result
	}))

	// Handler
	handler := CommandHandlerFunc(func(conn *Connection, cmd *Command) RedisValue {
		log = append(log, "HANDLER: Executing command")
		return RedisValue{Type: SimpleString, Str: "PONG"}
	})

	cmd := &Command{Name: "PING"}
	result := chain.Execute(nil, cmd, handler)

	if result.Type != SimpleString || result.Str != "PONG" {
		t.Errorf("Expected PONG, got %v", result)
	}

	// Verify the flow
	expectedLog := []string{
		"LOG: Command=PING",
		"METRICS: Recording command",
		"TIMING: Start",
		"HANDLER: Executing command",
		"TIMING: End",
		"METRICS: Command completed",
		"LOG: Result=0",
	}

	if len(log) != len(expectedLog) {
		t.Fatalf("Expected %d log entries, got %d", len(expectedLog), len(log))
	}

	for i, entry := range expectedLog {
		if log[i] != entry {
			t.Errorf("Log[%d]: expected '%s', got '%s'", i, entry, log[i])
		}
	}

	t.Logf("\nMiddleware chain execution flow:\n%s", strings.Join(log, "\n"))
}
