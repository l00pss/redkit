package redkit

import (
	"fmt"
	"io"
	"strconv"
)

/*
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

	switch value.Array[0].Type {
	case BulkString:
		cmd.Name = string(value.Array[0].Bulk)
	case SimpleString:
		cmd.Name = value.Array[0].Str
	default:
		return nil, fmt.Errorf("invalid command name type")
	}

	cmd.Args = make([]string, len(value.Array)-1)
	for i := 1; i < len(value.Array); i++ {
		switch value.Array[i].Type {
		case BulkString:
			cmd.Args[i-1] = string(value.Array[i].Bulk)
		case SimpleString:
			cmd.Args[i-1] = value.Array[i].Str
		default:
			return nil, fmt.Errorf("invalid argument type at index %d", i)
		}
	}

	return cmd, nil
}

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
// - Maximum size is 512MB (Redis default) to prevent DoS
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

	const maxBulkStringSize = 512 * 1024 * 1024
	if size > maxBulkStringSize {
		return RedisValue{}, fmt.Errorf("bulk string too large: %d bytes (max: %d)", size, maxBulkStringSize)
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
// - Maximum size is 1MB elements to prevent DoS
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

	const maxArraySize = 1024 * 1024 // 1M elements
	if size > maxArraySize {
		return RedisValue{}, fmt.Errorf("array too large: %d elements (max: %d)", size, maxArraySize)
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
