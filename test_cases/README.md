# RedKit Load Tests

## Quick Start

1. Start the server:
```bash
cd example
go run main.go
```

2. Run benchmark (recommended):
```bash
cd test_cases
chmod +x benchmark.sh
./benchmark.sh
```

## Test Scripts

### benchmark.sh (Recommended)
Uses `redis-benchmark` for comprehensive testing.

```bash
./benchmark.sh
```

Tests PING, SET, GET commands with 100k requests and 50 concurrent clients.

### load_test.sh
Custom shell script using `redis-cli` for testing implemented commands.

```bash
chmod +x load_test.sh
./load_test.sh
```

## Installation

### macOS
```bash
brew install redis  # For redis-cli and redis-benchmark
```

### Ubuntu/Debian
```bash
sudo apt-get install redis-tools
```

## Manual Testing

Test individual commands:
```bash
redis-cli -h localhost -p 6379 PING
redis-cli -h localhost -p 6379 HELLO
redis-cli -h localhost -p 6379 HELLO World
redis-cli -h localhost -p 6379 SET mykey myvalue
redis-cli -h localhost -p 6379 GET mykey
```

## Custom Benchmark

For specific workloads:
```bash
redis-benchmark -h localhost -p 6379 -t ping,set,get -n 100000 -c 50 -d 100
```

Parameters:
- `-n` - Total requests
- `-c` - Concurrent connections
- `-d` - Data size in bytes
- `-t` - Commands to test
