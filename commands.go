/*
Package redkit provides a comprehensive Redis-compatible server implementation.

This file defines all Redis command types and their registration helper functions.
The commands are organized into functional categories following Redis documentation.

Command Categories:
- Connection Commands: Basic server communication (PING, ECHO, QUIT)
- String Commands: String operations (GET, SET, INCR, APPEND, etc.)
- Hash Commands: Hash table operations (HGET, HSET, HKEYS, etc.)
- List Commands: List operations (LPUSH, RPOP, LRANGE, etc.)
- Set Commands: Set operations (SADD, SREM, SMEMBERS, etc.)
- Sorted Set Commands: Ordered set operations (ZADD, ZRANGE, ZSCORE, etc.)
- Stream Commands: Stream data structure operations (XADD, XREAD, etc.)
- Bitmap Commands: Bit manipulation (BITCOUNT, SETBIT, GETBIT, etc.)
- HyperLogLog Commands: Probabilistic cardinality estimation (PFADD, PFCOUNT, etc.)
- Geospatial Commands: Geographic operations (GEOADD, GEORADIUS, etc.)
- JSON Commands: JSON data operations (JSON.GET, JSON.SET, etc.)
- Search Commands: Full-text search (FT.SEARCH, FT.CREATE, etc.)
- Time Series Commands: Time-series data (TS.ADD, TS.RANGE, etc.)
- Vector Set Commands: Vector similarity operations (VADD, VSIM, etc.)
- Pub/Sub Commands: Message publishing/subscribing (PUBLISH, SUBSCRIBE, etc.)
- Transaction Commands: Atomic operations (MULTI, EXEC, WATCH, etc.)
- Scripting Commands: Lua script execution (EVAL, EVALSHA, etc.)
- Server Commands: Server management (INFO, CONFIG, SAVE, etc.)
- Cluster Commands: Redis cluster operations (CLUSTER, ASKING, etc.)
- Generic Commands: Key management (DEL, EXISTS, EXPIRE, TTL, etc.)

Usage Example:

	server := redkit.NewServer()

	// Register custom GET handler
	server.registerGetHandler(func(conn *Connection, cmd *Command) RedisValue {
		// Custom GET implementation
		return RedisValue{Type: SimpleString, Str: "value"}
	})

	// Start server
	server.ListenAndServe(":6379")

Each command has a corresponding registration helper function that follows the pattern:

	register{CommandName}Handler(f func(conn *Connection, cmd *Command) RedisValue)

This allows for easy customization and extension of command behavior while maintaining
Redis protocol compatibility.
*/
package redkit

// CommandType represents Redis command names as typed string constants
// This ensures type safety and provides intellisense support for command names
type CommandType string

/*
Redis Command Type Constants

All Redis commands are defined as strongly-typed constants to prevent typos
and provide IDE autocompletion. Commands are organized by functional category
matching the official Redis documentation structure.

The constants follow the exact Redis command names (case-sensitive) to ensure
protocol compatibility.
*/
const (
	// Connection Commands - Basic server communication
	PING CommandType = "PING" // Test server connectivity
	ECHO CommandType = "ECHO" // Echo the given string
	QUIT CommandType = "QUIT" // Close the connection
	HELP CommandType = "HELP" // Show help information

	// String Commands - Operations on string values
	APPEND      CommandType = "APPEND"      // Append a value to a key
	DECR        CommandType = "DECR"        // Decrement the integer value of a key by 1
	DECRBY      CommandType = "DECRBY"      // Decrement the integer value of a key by the given amount
	DELEX       CommandType = "DELEX"       // Delete key based on value comparison
	DIGEST      CommandType = "DIGEST"      // Return hash digest of a string value
	GET         CommandType = "GET"         // Get the value of a key
	GETDEL      CommandType = "GETDEL"      // Get the value of a key and delete the key
	GETEX       CommandType = "GETEX"       // Get the value of a key and set its expiration
	GETRANGE    CommandType = "GETRANGE"    // Get a substring of the string stored at a key
	GETSET      CommandType = "GETSET"      // Set the value of a key and return its old value
	INCR        CommandType = "INCR"        // Increment the integer value of a key by 1
	INCRBY      CommandType = "INCRBY"      // Increment the integer value of a key by the given amount
	INCRBYFLOAT CommandType = "INCRBYFLOAT" // Increment the float value of a key by the given amount
	LCS         CommandType = "LCS"         // Find the longest common substring
	MGET        CommandType = "MGET"        // Get the values of all the given keys
	MSET        CommandType = "MSET"        // Set multiple keys to multiple values
	MSETEX      CommandType = "MSETEX"      // Set multiple keys with expiration time
	MSETNX      CommandType = "MSETNX"      // Set multiple keys to multiple values, only if none exist
	PSETEX      CommandType = "PSETEX"
	SET         CommandType = "SET"
	SETEX       CommandType = "SETEX"
	SETNX       CommandType = "SETNX"
	SETRANGE    CommandType = "SETRANGE"
	STRLEN      CommandType = "STRLEN"
	SUBSTR      CommandType = "SUBSTR"

	//Hash Commands
	HDEL         CommandType = "HDEL"
	HEXISTS      CommandType = "HEXISTS"
	HEXPIRE      CommandType = "HEXPIRE"
	HEXPIREAT    CommandType = "HEXPIREAT"
	HEXPIRETIME  CommandType = "HEXPIRETIME"
	HGET         CommandType = "HGET"
	HGETALL      CommandType = "HGETALL"
	HGETDEL      CommandType = "HGETDEL"
	HGETEX       CommandType = "HGETEX"
	HINCRBY      CommandType = "HINCRBY"
	HINCRBYFLOAT CommandType = "HINCRBYFLOAT"
	HKEYS        CommandType = "HKEYS"
	HLEN         CommandType = "HLEN"
	HMGET        CommandType = "HMGET"
	HMSET        CommandType = "HMSET"
	HPERSIST     CommandType = "HPERSIST"
	HPEXPIRE     CommandType = "HPEXPIRE"
	HPEXPIREAT   CommandType = "HPEXPIREAT"
	HPEXPIRETIME CommandType = "HPEXPIRETIME"
	HPTTL        CommandType = "HPTTL"
	HRANDFIELD   CommandType = "HRANDFIELD"
	HSCAN        CommandType = "HSCAN"
	HSET         CommandType = "HSET"
	HSETEX       CommandType = "HSETEX"
	HSETNX       CommandType = "HSETNX"
	HSTRLEN      CommandType = "HSTRLEN"
	HTTL         CommandType = "HTTL"
	HVALS        CommandType = "HVALS"

	//List Commands
	BLMOVE     CommandType = "BLMOVE"
	BLMPOP     CommandType = "BLMPOP"
	BLPOP      CommandType = "BLPOP"
	BRPOP      CommandType = "BRPOP"
	BRPOPLPUSH CommandType = "BRPOPLPUSH"
	LINDEX     CommandType = "LINDEX"
	LINSERT    CommandType = "LINSERT"
	LLEN       CommandType = "LLEN"
	LMOVE      CommandType = "LMOVE"
	LMPOP      CommandType = "LMPOP"
	LPOP       CommandType = "LPOP"
	LPOS       CommandType = "LPOS"
	LPUSH      CommandType = "LPUSH"
	LPUSHX     CommandType = "LPUSHX"
	LRANGE     CommandType = "LRANGE"
	LREM       CommandType = "LREM"
	LSET       CommandType = "LSET"
	LTRIM      CommandType = "LTRIM"
	RPOP       CommandType = "RPOP"
	RPOPLPUSH  CommandType = "RPOPLPUSH"
	RPUSH      CommandType = "RPUSH"
	RPUSHX     CommandType = "RPUSHX"

	//Set Commands
	SADD        CommandType = "SADD"
	SCARD       CommandType = "SCARD"
	SDIFF       CommandType = "SDIFF"
	SDIFFSTORE  CommandType = "SDIFFSTORE"
	SINTER      CommandType = "SINTER"
	SINTERCARD  CommandType = "SINTERCARD"
	SINTERSTORE CommandType = "SINTERSTORE"
	SISMEMBER   CommandType = "SISMEMBER"
	SMEMBERS    CommandType = "SMEMBERS"
	SMISMEMBER  CommandType = "SMISMEMBER"
	SMOVE       CommandType = "SMOVE"
	SPOP        CommandType = "SPOP"
	SRANDMEMBER CommandType = "SRANDMEMBER"
	SREM        CommandType = "SREM"
	SSCAN       CommandType = "SSCAN"
	SUNION      CommandType = "SUNION"
	SUNIONSTORE CommandType = "SUNIONSTORE"

	//Sorted Set Commands
	BZMPOP           CommandType = "BZMPOP"
	BZPOPMAX         CommandType = "BZPOPMAX"
	BZPOPMIN         CommandType = "BZPOPMIN"
	ZADD             CommandType = "ZADD"
	ZCARD            CommandType = "ZCARD"
	ZCOUNT           CommandType = "ZCOUNT"
	ZDIFF            CommandType = "ZDIFF"
	ZDIFFSTORE       CommandType = "ZDIFFSTORE"
	ZINCRBY          CommandType = "ZINCRBY"
	ZINTER           CommandType = "ZINTER"
	ZINTERCARD       CommandType = "ZINTERCARD"
	ZINTERSTORE      CommandType = "ZINTERSTORE"
	ZLEXCOUNT        CommandType = "ZLEXCOUNT"
	ZMPOP            CommandType = "ZMPOP"
	ZMSCORE          CommandType = "ZMSCORE"
	ZPOPMAX          CommandType = "ZPOPMAX"
	ZPOPMIN          CommandType = "ZPOPMIN"
	ZRANDMEMBER      CommandType = "ZRANDMEMBER"
	ZRANGE           CommandType = "ZRANGE"
	ZRANGEBYLEX      CommandType = "ZRANGEBYLEX"
	ZRANGEBYSCORE    CommandType = "ZRANGEBYSCORE"
	ZRANGESTORE      CommandType = "ZRANGESTORE"
	ZRANK            CommandType = "ZRANK"
	ZREM             CommandType = "ZREM"
	ZREMRANGEBYLEX   CommandType = "ZREMRANGEBYLEX"
	ZREMRANGEBYRANK  CommandType = "ZREMRANGEBYRANK"
	ZREMRANGEBYSCORE CommandType = "ZREMRANGEBYSCORE"
	ZREVRANGE        CommandType = "ZREVRANGE"
	ZREVRANGEBYLEX   CommandType = "ZREVRANGEBYLEX"
	ZREVRANGEBYSCORE CommandType = "ZREVRANGEBYSCORE"
	ZREVRANK         CommandType = "ZREVRANK"
	ZSCAN            CommandType = "ZSCAN"
	ZSCORE           CommandType = "ZSCORE"
	ZUNION           CommandType = "ZUNION"
	ZUNIONSTORE      CommandType = "ZUNIONSTORE"

	//Stream Commands
	XACK       CommandType = "XACK"
	XACKDEL    CommandType = "XACKDEL"
	XADD       CommandType = "XADD"
	XAUTOCLAIM CommandType = "XAUTOCLAIM"
	XCLAIM     CommandType = "XCLAIM"
	XDEL       CommandType = "XDEL"
	XDELEX     CommandType = "XDELEX"
	XGROUP     CommandType = "XGROUP"
	XINFO      CommandType = "XINFO"
	XLEN       CommandType = "XLEN"
	XPENDING   CommandType = "XPENDING"
	XRANGE     CommandType = "XRANGE"
	XREAD      CommandType = "XREAD"
	XREADGROUP CommandType = "XREADGROUP"
	XREVRANGE  CommandType = "XREVRANGE"
	XSETID     CommandType = "XSETID"
	XTRIM      CommandType = "XTRIM"

	//Bitmap Commands
	BITCOUNT    CommandType = "BITCOUNT"
	BITFIELD    CommandType = "BITFIELD"
	BITFIELD_RO CommandType = "BITFIELD_RO"
	BITOP       CommandType = "BITOP"
	BITPOS      CommandType = "BITPOS"
	GETBIT      CommandType = "GETBIT"
	SETBIT      CommandType = "SETBIT"

	//HyperLogLog Commands
	PFADD      CommandType = "PFADD"
	PFCOUNT    CommandType = "PFCOUNT"
	PFDEBUG    CommandType = "PFDEBUG"
	PFMERGE    CommandType = "PFMERGE"
	PFSELFTEST CommandType = "PFSELFTEST"

	//Geospatial Commands
	GEOADD               CommandType = "GEOADD"
	GEODIST              CommandType = "GEODIST"
	GEOHASH              CommandType = "GEOHASH"
	GEOPOS               CommandType = "GEOPOS"
	GEORADIUS            CommandType = "GEORADIUS"
	GEORADIUSBYMEMBER    CommandType = "GEORADIUSBYMEMBER"
	GEORADIUSBYMEMBER_RO CommandType = "GEORADIUSBYMEMBER_RO"
	GEORADIUS_RO         CommandType = "GEORADIUS_RO"
	GEOSEARCH            CommandType = "GEOSEARCH"
	GEOSEARCHSTORE       CommandType = "GEOSEARCHSTORE"

	//JSON Commands
	JSON_ARRAPPEND CommandType = "JSON.ARRAPPEND"
	JSON_ARRINDEX  CommandType = "JSON.ARRINDEX"
	JSON_ARRINSERT CommandType = "JSON.ARRINSERT"
	JSON_ARRLEN    CommandType = "JSON.ARRLEN"
	JSON_ARRPOP    CommandType = "JSON.ARRPOP"
	JSON_ARRTRIM   CommandType = "JSON.ARRTRIM"
	JSON_CLEAR     CommandType = "JSON.CLEAR"
	JSON_DEBUG     CommandType = "JSON.DEBUG"
	JSON_DEL       CommandType = "JSON.DEL"
	JSON_FORGET    CommandType = "JSON.FORGET"
	JSON_GET       CommandType = "JSON.GET"
	JSON_MERGE     CommandType = "JSON.MERGE"
	JSON_MGET      CommandType = "JSON.MGET"
	JSON_MSET      CommandType = "JSON.MSET"
	JSON_NUMINCRBY CommandType = "JSON.NUMINCRBY"
	JSON_NUMMULTBY CommandType = "JSON.NUMMULTBY"
	JSON_OBJKEYS   CommandType = "JSON.OBJKEYS"
	JSON_OBJLEN    CommandType = "JSON.OBJLEN"
	JSON_RESP      CommandType = "JSON.RESP"
	JSON_SET       CommandType = "JSON.SET"
	JSON_STRAPPEND CommandType = "JSON.STRAPPEND"
	JSON_STRLEN    CommandType = "JSON.STRLEN"
	JSON_TOGGLE    CommandType = "JSON.TOGGLE"
	JSON_TYPE      CommandType = "JSON.TYPE"

	//Search Commands
	FT_AGGREGATE   CommandType = "FT.AGGREGATE"
	FT_ALIASADD    CommandType = "FT.ALIASADD"
	FT_ALIASDEL    CommandType = "FT.ALIASDEL"
	FT_ALIASUPDATE CommandType = "FT.ALIASUPDATE"
	FT_ALTER       CommandType = "FT.ALTER"
	FT_CONFIG      CommandType = "FT.CONFIG"
	FT_CREATE      CommandType = "FT.CREATE"
	FT_CURSOR      CommandType = "FT.CURSOR"
	FT_DICTADD     CommandType = "FT.DICTADD"
	FT_DICTDEL     CommandType = "FT.DICTDEL"
	FT_DICTDUMP    CommandType = "FT.DICTDUMP"
	FT_DROPINDEX   CommandType = "FT.DROPINDEX"
	FT_EXPLAIN     CommandType = "FT.EXPLAIN"
	FT_EXPLAINCLI  CommandType = "FT.EXPLAINCLI"
	FT_HYBRID      CommandType = "FT.HYBRID"
	FT_INFO        CommandType = "FT.INFO"
	FT_PROFILE     CommandType = "FT.PROFILE"
	FT_SEARCH      CommandType = "FT.SEARCH"
	FT_SPELLCHECK  CommandType = "FT.SPELLCHECK"
	FT_SYNDUMP     CommandType = "FT.SYNDUMP"
	FT_SYNUPDATE   CommandType = "FT.SYNUPDATE"
	FT_TAGVALS     CommandType = "FT.TAGVALS"
	FT_LIST        CommandType = "FT._LIST"

	//Time Series Commands
	TS_ADD        CommandType = "TS.ADD"
	TS_ALTER      CommandType = "TS.ALTER"
	TS_CREATE     CommandType = "TS.CREATE"
	TS_CREATERULE CommandType = "TS.CREATERULE"
	TS_DECRBY     CommandType = "TS.DECRBY"
	TS_DEL        CommandType = "TS.DEL"
	TS_DELETERULE CommandType = "TS.DELETERULE"
	TS_GET        CommandType = "TS.GET"
	TS_INCRBY     CommandType = "TS.INCRBY"
	TS_INFO       CommandType = "TS.INFO"
	TS_MADD       CommandType = "TS.MADD"
	TS_MGET       CommandType = "TS.MGET"
	TS_MRANGE     CommandType = "TS.MRANGE"
	TS_MREVRANGE  CommandType = "TS.MREVRANGE"
	TS_QUERYINDEX CommandType = "TS.QUERYINDEX"
	TS_RANGE      CommandType = "TS.RANGE"
	TS_REVRANGE   CommandType = "TS.REVRANGE"

	//Vector Set Commands
	VADD        CommandType = "VADD"
	VCARD       CommandType = "VCARD"
	VDIM        CommandType = "VDIM"
	VEMB        CommandType = "VEMB"
	VGETATTR    CommandType = "VGETATTR"
	VINFO       CommandType = "VINFO"
	VISMEMBER   CommandType = "VISMEMBER"
	VLINKS      CommandType = "VLINKS"
	VRANDMEMBER CommandType = "VRANDMEMBER"
	VRANGE      CommandType = "VRANGE"
	VREM        CommandType = "VREM"
	VSETATTR    CommandType = "VSETATTR"
	VSIM        CommandType = "VSIM"

	//Pub/Sub Commands
	PSUBSCRIBE   CommandType = "PSUBSCRIBE"
	PUBLISH      CommandType = "PUBLISH"
	PUBSUB       CommandType = "PUBSUB"
	PUNSUBSCRIBE CommandType = "PUNSUBSCRIBE"
	SPUBLISH     CommandType = "SPUBLISH"
	SSUBSCRIBE   CommandType = "SSUBSCRIBE"
	SUBSCRIBE    CommandType = "SUBSCRIBE"
	SUNSUBSCRIBE CommandType = "SUNSUBSCRIBE"
	UNSUBSCRIBE  CommandType = "UNSUBSCRIBE"

	//Transaction Commands
	DISCARD CommandType = "DISCARD"
	EXEC    CommandType = "EXEC"
	MULTI   CommandType = "MULTI"
	UNWATCH CommandType = "UNWATCH"
	WATCH   CommandType = "WATCH"

	//Scripting Commands
	EVAL       CommandType = "EVAL"
	EVALSHA    CommandType = "EVALSHA"
	EVALSHA_RO CommandType = "EVALSHA_RO"
	EVAL_RO    CommandType = "EVAL_RO"
	FCALL      CommandType = "FCALL"
	FCALL_RO   CommandType = "FCALL_RO"
	FUNCTION   CommandType = "FUNCTION"
	SCRIPT     CommandType = "SCRIPT"

	//Connection Commands
	AUTH   CommandType = "AUTH"
	CLIENT CommandType = "CLIENT"
	HELLO  CommandType = "HELLO"
	RESET  CommandType = "RESET"

	//Server Commands
	ACL            CommandType = "ACL"
	BGREWRITEAOF   CommandType = "BGREWRITEAOF"
	BGSAVE         CommandType = "BGSAVE"
	COMMAND        CommandType = "COMMAND"
	CONFIG         CommandType = "CONFIG"
	DBSIZE         CommandType = "DBSIZE"
	FAILOVER       CommandType = "FAILOVER"
	FLUSHALL       CommandType = "FLUSHALL"
	FLUSHDB        CommandType = "FLUSHDB"
	INFO           CommandType = "INFO"
	LASTSAVE       CommandType = "LASTSAVE"
	LATENCY        CommandType = "LATENCY"
	LOLWUT         CommandType = "LOLWUT"
	MEMORY         CommandType = "MEMORY"
	MODULE         CommandType = "MODULE"
	MONITOR        CommandType = "MONITOR"
	PSYNC          CommandType = "PSYNC"
	REPLCONF       CommandType = "REPLCONF"
	REPLICAOF      CommandType = "REPLICAOF"
	RESTORE_ASKING CommandType = "RESTORE-ASKING"
	ROLE           CommandType = "ROLE"
	SAVE           CommandType = "SAVE"
	SHUTDOWN       CommandType = "SHUTDOWN"
	SLAVEOF        CommandType = "SLAVEOF"
	SLOWLOG        CommandType = "SLOWLOG"
	SWAPDB         CommandType = "SWAPDB"
	SYNC           CommandType = "SYNC"
	TIME           CommandType = "TIME"

	//Cluster Commands
	ASKING    CommandType = "ASKING"
	CLUSTER   CommandType = "CLUSTER"
	READONLY  CommandType = "READONLY"
	READWRITE CommandType = "READWRITE"

	//Generic Commands
	COPY        CommandType = "COPY"
	DEL         CommandType = "DEL"
	DUMP        CommandType = "DUMP"
	EXISTS      CommandType = "EXISTS"
	EXPIRE      CommandType = "EXPIRE"
	EXPIREAT    CommandType = "EXPIREAT"
	EXPIRETIME  CommandType = "EXPIRETIME"
	KEYS        CommandType = "KEYS"
	MIGRATE     CommandType = "MIGRATE"
	MOVE        CommandType = "MOVE"
	OBJECT      CommandType = "OBJECT"
	PERSIST     CommandType = "PERSIST"
	PEXPIRE     CommandType = "PEXPIRE"
	PEXPIREAT   CommandType = "PEXPIREAT"
	PEXPIRETIME CommandType = "PEXPIRETIME"
	PTTL        CommandType = "PTTL"
	RANDOMKEY   CommandType = "RANDOMKEY"
	RENAME      CommandType = "RENAME"
	RENAMENX    CommandType = "RENAMENX"
	RESTORE     CommandType = "RESTORE"
	SCAN        CommandType = "SCAN"
	SORT        CommandType = "SORT"
	SORT_RO     CommandType = "SORT_RO"
	TOUCH       CommandType = "TOUCH"
	TTL         CommandType = "TTL"
	TYPE        CommandType = "TYPE"
	UNLINK      CommandType = "UNLINK"
	WAIT        CommandType = "WAIT"
	WAITAOF     CommandType = "WAITAOF"
)

/*
Default Command Handlers

registerDefaultHandlers sets up the basic Redis protocol commands that are
essential for client connectivity and server interaction. These handlers
implement the minimum functionality required for Redis compatibility:

- PING: Connectivity testing with optional message echo
- ECHO: Simple string echo for testing
- HELP: Basic command information
- QUIT: Graceful connection termination

Custom implementations can override these by registering new handlers
with the same command names, or extend functionality by registering
additional commands.
*/

// registerDefaultHandlers registers the built-in Redis commands
func (s *Server) registerDefaultHandlers() {
	// PING command
	s.RegisterCommandFunc(string(PING), func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) == 0 {
			return RedisValue{Type: SimpleString, Str: "PONG"}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
	})

	// ECHO command
	s.RegisterCommandFunc(string(ECHO), func(conn *Connection, cmd *Command) RedisValue {
		if len(cmd.Args) != 1 {
			return RedisValue{Type: ErrorReply, Str: "ERR wrong number of arguments for 'echo' command"}
		}
		return RedisValue{Type: BulkString, Bulk: []byte(cmd.Args[0])}
	})

	s.RegisterCommandFunc(string(HELP), func(conn *Connection, cmd *Command) RedisValue {
		helpText := "RedKit Redis Server - Supported commands:\n" +
			"PING [message] - Returns PONG or the provided message\n" +
			"ECHO message - Echoes the provided message\n" +
			"QUIT - Closes the connection\n" +
			"(Other commands may be supported depending on the server configuration)"
		return RedisValue{Type: BulkString, Bulk: []byte(helpText)}
	})

	// QUIT command
	s.RegisterCommandFunc(string(QUIT), func(conn *Connection, cmd *Command) RedisValue {
		err := conn.Close()
		if err != nil {
			return RedisValue{}
		}
		return RedisValue{Type: SimpleString, Str: "OK"}
	})
}

/*
Command Registration Helper Functions

These functions provide a convenient way to register custom handlers for Redis commands.
Each function follows the pattern: register{CommandName}Handler(handlerFunc)

The handler function signature is always:
	func(conn *Connection, cmd *Command) RedisValue

Where:
- conn: The client connection context
- cmd: The parsed command with arguments
- RedisValue: The response to send back to the client

Example usage:
	server.registerGetHandler(func(conn *Connection, cmd *Command) RedisValue {
		key := cmd.Args[0]
		value := myStorage.Get(key)
		return RedisValue{Type: BulkString, Bulk: []byte(value)}
	})
*/

// registerPingHandler registers a custom handler for the PING command
// PING [message] - Test connectivity and optionally echo a message
func (s *Server) registerPingHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc(string(PING), f)
}

// registerEchoHandler registers a custom handler for the ECHO command
// ECHO message - Return the given string
func (s *Server) registerEchoHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc(string(ECHO), f)
}

// registerQuitHandler registers a custom handler for the QUIT command
// QUIT - Close the connection
func (s *Server) registerQuitHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc(string(QUIT), f)
}

// registerHelpHandler registers a custom handler for the HELP command
// HELP - Show available commands and their descriptions
func (s *Server) registerHelpHandler(f func(conn *Connection, cmd *Command) RedisValue) {
	s.RegisterCommandFunc(string(HELP), f)
}
