#!/bin/bash
# UNN SSH Wrapper - Teleport to rooms using ssh:// URLs

cd "$(dirname "$0")"

# Build if needed
if [ ! -f unn-ssh-bin ] || [ cmd/unn-ssh/main.go -nt unn-ssh-bin ]; then
    echo "Building unn-ssh..."
    go build -o unn-ssh-bin ./cmd/unn-ssh
fi

# Run the wrapper
./unn-ssh-bin "$@"
