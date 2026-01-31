#!/bin/bash
# UNN Client - Teleport to rooms using unn:// URLs

cd "$(dirname "$0")"

# Build if needed
if [ ! -f unn-client-bin ] || [ "$(find cmd/unn-client -name '*.go' -newer unn-client-bin)" ]; then
    echo "Building unn-client..."
    go build -o unn-client-bin ./cmd/unn-client
fi

if [ ! -f unn-dl-bin ] || [ "$(find cmd/unn-dl -name '*.go' -newer unn-dl-bin)" ]; then
    echo "Building unn-dl..."
    go build -o unn-dl-bin ./cmd/unn-dl
fi

# Run the client
./unn-client-bin "$@"
