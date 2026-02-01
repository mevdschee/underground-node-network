#!/bin/bash
# UNN Client - Teleport to rooms using unn:// URLs

cd "$(dirname "$0")"

# Build if needed
if [ ! -f unn-client-bin ] || [ "$(find cmd/unn-client -name '*.go' -newer unn-client-bin)" ]; then
    echo "Building unn-client..."
    go build -o unn-client-bin ./cmd/unn-client
fi

# Run the client
./unn-client-bin "$@"
