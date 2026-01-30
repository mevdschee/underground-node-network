#!/bin/bash
# UNN SSH Wrapper - Teleport to rooms using ssh:// URLs

cd "$(dirname "$0")"

# Build if needed
if [ ! -f unn-client-bin ] || [ cmd/unn-client/main.go -nt unn-client-bin ]; then
    echo "Building unn-client..."
    go build -o unn-client-bin ./cmd/unn-client
fi

if [ ! -f unn-dl-bin ] || [ cmd/unn-dl/main.go -nt unn-dl-bin ]; then
    echo "Building unn-dl..."
    go build -o unn-dl-bin ./cmd/unn-dl
fi

# Run the client
./unn-client-bin "$@"
