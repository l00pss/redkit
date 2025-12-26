package redkit

import (
	"fmt"
	"io"
	"strconv"
)

// readCommand reads and parses a Redis command from the connection
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
		return RedisValue{}, fmt.Errorf("invalid RESP protocol indicator: %c (0x%02x)", line[0], line[0])
	}
}

// readLine reads a CRLF-terminated line
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

// readBulkString reads a bulk string with length prefix
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

	return RedisValue{Type: BulkString, Bulk: data[:size]}, nil
}

// readArray reads an array of Redis values
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

// writeValue writes a Redis value to the connection in RESP format
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
