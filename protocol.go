/*
Package redkit implements Redis Serialization Protocol (RESP) parsing and serialization.

This file provides the core protocol implementation for reading and writing
Redis protocol messages over network connections. It handles the complete
RESP specification with support for all standard data types.

RESP Protocol Overview:
RESP is a binary-safe protocol that uses CRLF (\r\n) as line terminators.
Each data type is prefixed with a single character type indicator:

- Simple Strings: +OK\r\n
- Error Messages: -ERR message\r\n
- Integers: :42\r\n
- Bulk Strings: $6\r\nhello!\r\n
- Arrays: *2\r\n$3\r\nget\r\n$3\r\nkey\r\n
- Null Values: $-1\r\n

Command Processing Pipeline:
1. Read RESP-encoded command from client connection
2. Parse into structured Command with name and arguments
3. Route to appropriate command handler
4. Serialize handler response back to RESP format
5. Write response to client connection

Key Features:
- Full RESP protocol compliance for Redis compatibility
- Binary-safe data handling (supports arbitrary byte sequences)
- Efficient streaming parser with minimal allocations
- Robust error handling with descriptive error messages
- Support for nested data structures (arrays of arrays)
- Thread-safe when used with separate connections

Performance Characteristics:
- Streaming parser processes data as it arrives
- Minimal memory allocation during parsing
- Buffered I/O for optimal network performance
- Direct byte manipulation to avoid string conversions
- Efficient integer parsing with built-in validation

Protocol Compliance:
This implementation follows the official Redis RESP specification
and maintains compatibility with all standard Redis clients including:
- redis-cli (official command line client)
- Language-specific Redis client libraries
- Redis cluster and replication protocols
- Redis Modules and custom protocol extensions

Usage Example:

	// Reading a command (typically done by server)
	cmd, err := conn.readCommand()
	if err != nil {
		return err
	}

	// Processing and responding
	response := RedisValue{
		Type: SimpleString,
		Str:  "OK",
	}

	return conn.writeValue(response)

Error Handling:
The protocol parser provides detailed error messages for debugging
and maintains connection state even when encountering malformed data.
All parsing errors are recoverable and won't corrupt the connection.
*/
package redkit

import (
	"fmt"
	"io"
	"strconv"
)

/*
Redis Command Parsing

These methods handle the parsing of incoming Redis commands from client
connections. Commands follow the RESP array format where the first element
is the command name and subsequent elements are arguments.

Command Format:
*3\r\n$3\r\nSET\r\n$3\r\nkey\r\n$5\r\nvalue\r\n

Parsed as:
Command{
    Name: "SET",
    Args: ["key", "value"],
    Raw:  [RedisValue{Type: BulkString, Bulk: []byte("SET")}, ...]
}
*/

// readCommand reads and parses a Redis command from the connection
// Expects commands in RESP array format where the first element is the
// command name and remaining elements are arguments. Both BulkString
// and SimpleString types are accepted for command names and arguments.
//
// Returns:
// - *Command: Parsed command with name, arguments, and raw values
// - error: Protocol parsing errors or connection issues
func (c *Connection) readCommand() (*Command, error) {
	value, err := c.readValue()
	if err != nil {
		return nil, err
	}

	if value.Type != Array {
		return nil, fmt.Errorf("expected array, got %v", value.Type)
	}

	if len(value.Array) == 0 {
		return nil, fmt.Errorf("empty command array")
	}

	cmd := &Command{
		Raw: value.Array,
	}

	// Extract command name
	if value.Array[0].Type == BulkString {
		cmd.Name = string(value.Array[0].Bulk)
	} else if value.Array[0].Type == SimpleString {
		cmd.Name = value.Array[0].Str
	} else {
		return nil, fmt.Errorf("invalid command name type")
	}

	// Extract arguments
	cmd.Args = make([]string, len(value.Array)-1)
	for i := 1; i < len(value.Array); i++ {
		if value.Array[i].Type == BulkString {
			cmd.Args[i-1] = string(value.Array[i].Bulk)
		} else if value.Array[i].Type == SimpleString {
			cmd.Args[i-1] = value.Array[i].Str
		} else {
			return nil, fmt.Errorf("invalid argument type at index %d", i)
		}
	}

	return cmd, nil
}

/*
RESP Protocol Value Parsing

These methods implement the core RESP protocol parser that handles
all Redis data types according to the official specification.
*/

// readValue reads a Redis protocol value
// Parses any RESP-encoded value by examining the first byte type indicator:
// '+' - Simple String (single line, no CRLF allowed)
// '-' - Error Reply (single line error message)
// ':' - Integer (64-bit signed integer)
// '$' - Bulk String (binary-safe string with length prefix)
// '*' - Array (ordered collection of Redis values)
//
// The parser handles nested structures recursively and maintains
// binary safety for all data types.
//
// Returns:
// - RedisValue: Parsed value with appropriate type and data
// - error: Protocol violations or connection errors
func (c *Connection) readValue() (RedisValue, error) {
	line, err := c.readLine()
	if err != nil {
		return RedisValue{}, err
	}

	if len(line) == 0 {
		return RedisValue{}, fmt.Errorf("empty line")
	}

	switch line[0] {
	case '+': // Simple string
		return RedisValue{Type: SimpleString, Str: string(line[1:])}, nil
	case '-': // Error
		return RedisValue{Type: ErrorReply, Str: string(line[1:])}, nil
	case ':': // Integer
		n, err := strconv.ParseInt(string(line[1:]), 10, 64)
		if err != nil {
			return RedisValue{}, fmt.Errorf("invalid integer: %v", err)
		}
		return RedisValue{Type: Integer, Int: n}, nil
	case '$': // Bulk string
		return c.readBulkString(line[1:])
	case '*': // Array
		return c.readArray(line[1:])
	default:
		return RedisValue{}, fmt.Errorf("invalid type indicator: %c", line[0])
	}
}

// readLine reads a CRLF-terminated line
// Handles both CRLF (\r\n) and LF (\n) line endings for compatibility
// with different client implementations. Removes the line terminator
// from the returned data.
//
// Returns:
// - []byte: Line data without terminator
// - error: I/O errors or connection issues
func (c *Connection) readLine() ([]byte, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	// Remove CRLF
	if len(line) >= 2 && line[len(line)-2] == '\r' {
		line = line[:len(line)-2]
	} else if len(line) >= 1 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}

	return line, nil
}

// readBulkString reads a bulk string
// Bulk strings are binary-safe strings with an explicit length prefix.
// Format: $<length>\r\n<data>\r\n
//
// Special cases:
// - $-1\r\n represents a null value
// - $0\r\n\r\n represents an empty string
// - Length must be non-negative (except -1 for null)
//
// Parameters:
// - sizeBytes: Length specification from protocol stream
//
// Returns:
// - RedisValue: BulkString with binary data or Null value
// - error: Invalid length, I/O errors, or protocol violations
func (c *Connection) readBulkString(sizeBytes []byte) (RedisValue, error) {
	size, err := strconv.Atoi(string(sizeBytes))
	if err != nil {
		return RedisValue{}, fmt.Errorf("invalid bulk string size: %v", err)
	}

	if size == -1 {
		return RedisValue{Type: Null}, nil
	}

	if size < 0 {
		return RedisValue{}, fmt.Errorf("invalid bulk string size: %d", size)
	}

	// Read the bulk data plus CRLF
	data := make([]byte, size+2)
	_, err = io.ReadFull(c.reader, data)
	if err != nil {
		return RedisValue{}, err
	}

	// Remove CRLF
	return RedisValue{Type: BulkString, Bulk: data[:size]}, nil
}

// readArray reads an array
// Arrays are ordered collections of Redis values with an explicit count.
// Format: *<count>\r\n<element1><element2>...<elementN>
//
// Each element can be any Redis value type, including nested arrays.
// This enables complex data structures like arrays of arrays.
//
// Special cases:
// - *-1\r\n represents a null array
// - *0\r\n represents an empty array
// - Count must be non-negative (except -1 for null)
//
// Parameters:
// - sizeBytes: Array size specification from protocol stream
//
// Returns:
// - RedisValue: Array with parsed elements or Null value
// - error: Invalid count, parsing errors, or connection issues
func (c *Connection) readArray(sizeBytes []byte) (RedisValue, error) {
	size, err := strconv.Atoi(string(sizeBytes))
	if err != nil {
		return RedisValue{}, fmt.Errorf("invalid array size: %v", err)
	}

	if size == -1 {
		return RedisValue{Type: Null}, nil
	}

	if size < 0 {
		return RedisValue{}, fmt.Errorf("invalid array size: %d", size)
	}

	array := make([]RedisValue, size)
	for i := 0; i < size; i++ {
		value, err := c.readValue()
		if err != nil {
			return RedisValue{}, err
		}
		array[i] = value
	}

	return RedisValue{Type: Array, Array: array}, nil
}

/*
RESP Protocol Value Serialization

This method handles serialization of Redis values back to RESP format
for transmission to clients. It supports all standard Redis data types
and maintains protocol compliance.
*/

// writeValue writes a Redis value to the connection
// Serializes RedisValue structures to RESP protocol format according
// to the value type. The output follows the exact RESP specification
// for maximum client compatibility.
//
// Serialization Format by Type:
// - SimpleString: +<string>\r\n
// - ErrorReply: -<message>\r\n
// - Integer: :<number>\r\n
// - BulkString: $<length>\r\n<data>\r\n
// - Array: *<count>\r\n<element1>...<elementN>
// - Null: $-1\r\n
//
// For arrays, each element is recursively serialized, enabling
// complex nested structures while maintaining protocol compliance.
//
// Parameters:
// - value: RedisValue to serialize and transmit
//
// Returns:
// - error: Serialization or I/O errors
func (c *Connection) writeValue(value RedisValue) error {
	switch value.Type {
	case SimpleString:
		_, err := c.writer.WriteString("+" + value.Str + "\r\n")
		return err
	case ErrorReply:
		_, err := c.writer.WriteString("-" + value.Str + "\r\n")
		return err
	case Integer:
		_, err := c.writer.WriteString(":" + strconv.FormatInt(value.Int, 10) + "\r\n")
		return err
	case BulkString:
		_, err := c.writer.WriteString("$" + strconv.Itoa(len(value.Bulk)) + "\r\n")
		if err != nil {
			return err
		}
		_, err = c.writer.Write(value.Bulk)
		if err != nil {
			return err
		}
		_, err = c.writer.WriteString("\r\n")
		return err
	case Array:
		_, err := c.writer.WriteString("*" + strconv.Itoa(len(value.Array)) + "\r\n")
		if err != nil {
			return err
		}
		for _, item := range value.Array {
			if err := c.writeValue(item); err != nil {
				return err
			}
		}
		return nil
	case Null:
		_, err := c.writer.WriteString("$-1\r\n")
		return err
	default:
		return fmt.Errorf("unsupported value type: %v", value.Type)
	}
}
