#!/bin/bash

# RedKit Load Test Script using redis-cli
# Tests: PING, HELLO, SET, GET commands

HOST="localhost"
PORT="6379"
ITERATIONS=10000
CONCURRENT=50

echo "=== RedKit Load Test ==="
echo "Host: $HOST:$PORT"
echo "Iterations: $ITERATIONS"
echo "Concurrent connections: $CONCURRENT"
echo ""

# Test if server is running
if ! redis-cli -h $HOST -p $PORT PING > /dev/null 2>&1; then
    echo "Error: Cannot connect to Redis server at $HOST:$PORT"
    echo "Make sure the server is running: cd example && go run main.go"
    exit 1
fi

echo "✓ Server is running"
echo ""

# Function to run test
run_test() {
    local cmd=$1
    local name=$2
    echo "Testing $name..."
    redis-cli -h $HOST -p $PORT --intrinsic-latency 1 > /dev/null 2>&1
    start=$(date +%s%N)
    
    for i in $(seq 1 $ITERATIONS); do
        redis-cli -h $HOST -p $PORT $cmd > /dev/null 2>&1 &
        
        # Limit concurrent connections
        if (( i % CONCURRENT == 0 )); then
            wait
        fi
    done
    wait
    
    end=$(date +%s%N)
    duration=$(( (end - start) / 1000000 ))
    rps=$(( ITERATIONS * 1000 / duration ))
    avg_latency=$(echo "scale=2; $duration / $ITERATIONS" | bc)
    
    echo "  ✓ Completed: $ITERATIONS requests in ${duration}ms"
    echo "  ✓ Throughput: ~$rps req/sec"
    echo "  ✓ Avg latency: ${avg_latency}ms"
    echo ""
}

# Run tests
echo "=== Running Tests ==="
echo ""

run_test "PING" "PING command"
run_test "HELLO" "HELLO command"

# SET test
echo "Testing SET command..."
start=$(date +%s%N)
for i in $(seq 1 $ITERATIONS); do
    redis-cli -h $HOST -p $PORT SET "test:key:$i" "value:$i" > /dev/null 2>&1 &
    if (( i % CONCURRENT == 0 )); then
        wait
    fi
done
wait
end=$(date +%s%N)
duration=$(( (end - start) / 1000000 ))
rps=$(( ITERATIONS * 1000 / duration ))
avg_latency=$(echo "scale=2; $duration / $ITERATIONS" | bc)
echo "  ✓ Completed: $ITERATIONS requests in ${duration}ms"
echo "  ✓ Throughput: ~$rps req/sec"
echo "  ✓ Avg latency: ${avg_latency}ms"
echo ""

# GET test
echo "Testing GET command..."
start=$(date +%s%N)
for i in $(seq 1 $ITERATIONS); do
    redis-cli -h $HOST -p $PORT GET "test:key:$i" > /dev/null 2>&1 &
    if (( i % CONCURRENT == 0 )); then
        wait
    fi
done
wait
end=$(date +%s%N)
duration=$(( (end - start) / 1000000 ))
rps=$(( ITERATIONS * 1000 / duration ))
avg_latency=$(echo "scale=2; $duration / $ITERATIONS" | bc)
echo "  ✓ Completed: $ITERATIONS requests in ${duration}ms"
echo "  ✓ Throughput: ~$rps req/sec"
echo "  ✓ Avg latency: ${avg_latency}ms"
echo ""

echo "=== Test Complete ==="
