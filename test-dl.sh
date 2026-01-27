#!/bin/bash
set -e

# 1. Build components
echo "Building test server and downloader..."
go build -o test-dl-server-bin ./cmd/test-dl-server
go build -o unn-dl-bin ./cmd/unn-dl

# 2. Kill existing processes on port 44323
fuser -k 44323/tcp || true

# 3. Start test server in background
echo "Starting standalone SFTP test server on 127.0.0.1:44323 (serving ./room_files)..."
./test-dl-server-bin ./room_files &
SERVER_PID=$!

# 4. cleanup on exit
trap "kill $SERVER_PID || true; rm -f test-dl-server-bin" EXIT

# Give server a moment to start
sleep 1

# 5. Determine which file to download and calculate its signature
TEST_FILE="test_256.bin"
if [ ! -f "./room_files/$TEST_FILE" ]; then
    # Fallback to any file in room_files
    TEST_FILE=$(ls ./room_files | head -n 1)
fi

if [ -z "$TEST_FILE" ]; then
    echo "Error: No files found in ./room_files to test with."
    exit 1
fi

SIG=$(sha256sum "./room_files/$TEST_FILE" | awk '{print $1}')
echo "Launching unn-dl to download '$TEST_FILE' (sig: $SIG)..."
./unn-dl-bin -port 44323 -id "$TEST_FILE" -file "$TEST_FILE" -sig "$SIG"

echo -e "\nTest finished."
