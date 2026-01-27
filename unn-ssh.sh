#!/bin/bash
# UNN SSH Wrapper - Teleport to rooms using ssh:// URLs

cd "$(dirname "$0")"

# Build if needed
if [ ! -f unn-ssh-bin ] || [ cmd/unn-ssh/main.go -nt unn-ssh-bin ]; then
    echo "Building unn-ssh..."
    go build -o unn-ssh-bin ./cmd/unn-ssh
fi

if [ ! -f unn-dl-bin ] || [ cmd/unn-dl/main.go -nt unn-dl-bin ]; then
    echo "Building unn-dl..."
    go build -o unn-dl-bin ./cmd/unn-dl
fi

# Run the wrapper
./unn-ssh-bin "$@"
