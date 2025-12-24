package redkit

// registerDefaultHandlers registers the built-in Redis commands
func (s *Server) registerDefaultHandlers() {
	// PING command
	s.RegisterCommandFunc("PING", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) == 0 {
			return RedisValue{Type: SimpleString, Str: "PONG"}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
	})

	// ECHO command
	s.RegisterCommandFunc("ECHO", func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'echo' command"}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
	})

	s.RegisterCommandFunc("HELP", func(conn *Connection, cmd *Command) RedisValue {
		helpText := "RedKit Redis Server - Supported commands:\n" +
			"PING [message] - Returns PONG or the provided message\n" +
			"ECHO message - Echoes the provided message\n" +
			"QUIT - Closes the connection\n" +
			"(Other commands may be supported depending on the server configuration)"
		return RedisValue{Type: BulkString, Bulk: []byte(helpText)}
	})

	// QUIT command
	s.RegisterCommandFunc("QUIT", func(conn *Connection, cmd *Command) RedisValue {
		err := conn.Close()
		if err != nil {
			return RedisValue{}
		}
		return RedisValue{Type: SimpleString, Str: "OK"}
	})
}

func (s *Server) registerGetHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("GET", f)
}

func (s *Server) registerSetHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("SET", f)
}

func (s *Server) registerDelHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("DEL", f)
}

func (s *Server) registerExistsHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("EXISTS", f)
}

func (s *Server) registerIncrHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("INCR", f)
}

func (s *Server) registerDecrHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("DECR", f)
}

func (s *Server) registerExpireHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("EXPIRE", f)
}

func (s *Server) registerTTLHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("TTL", f)
}

func (s *Server) registerKeysHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("KEYS", f)
}

func (s *Server) registerFlushAllHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("FLUSHALL", f)
}

func (s *Server) registerFlushDBHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("FLUSHDB", f)
}

func (s *Server) registerSelectHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("SELECT", f)
}

func (s *Server) registerAuthHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("AUTH", f)
}

func (s *Server) registerDbSizeHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc("DBSIZE", f)
}
