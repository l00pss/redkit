#!/bin/bash

# Quick benchmark script for RedKit server
# Run this after starting the server to verify performance

HOST="localhost"
PORT="6379"
REQUESTS=50000
CLIENTS=25

echo "Starting RedKit performance test..."
echo "Server: $HOST:$PORT"
echo ""

# Check if redis tools are available
if ! command -v redis-benchmark &> /dev/null; then
    echo "Please install redis-tools first"
    echo "On Mac: brew install redis"
    exit 1
fi

# Make sure server is up
if ! redis-cli -h $HOST -p $PORT PING > /dev/null 2>&1; then
    echo "Can't connect to server on $HOST:$PORT"
    echo "Start it with: cd example && go run main.go"
    exit 1
fi

echo "Connected to server"
echo ""

# Test basic PING
echo "Testing PING..."
redis-benchmark -h $HOST -p $PORT -t ping -n $REQUESTS -c $CLIENTS -q
echo ""

# Test SET operations
echo "Testing SET with 100 byte values..."
redis-benchmark -h $HOST -p $PORT -t set -n $REQUESTS -c $CLIENTS -q -d 100
echo ""

# Test GET operations  
echo "Testing GET..."
redis-benchmark -h $HOST -p $PORT -t get -n $REQUESTS -c $CLIENTS -q -d 100
echo ""

# Try the custom HELLO command
echo "Testing custom HELLO command..."
names=("Alice" "Bob" "Charlie" "Dave" "Eve")
for name in "${names[@]}"; do
    result=$(redis-cli -h $HOST -p $PORT HELLO "$name" 2>&1)
    if [[ $result == *"Hello"* ]]; then
        echo "  $name: $result"
    else
        echo "  $name: failed"
    fi
done
echo ""

echo "Benchmark finished!"
echo "Run 'redis-benchmark -h $HOST -p $PORT -t ping,set,get --csv' for detailed stats"