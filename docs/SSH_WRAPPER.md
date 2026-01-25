# UNN SSH Wrapper

The `unn-ssh` wrapper provides a convenient way to "teleport" directly to UNN rooms using `ssh://` URLs.

## Overview

Instead of manually connecting to the entry point, navigating the menu, and then copying connection details, the wrapper automates the entire process:

1. Parses the `ssh://` URL to extract entry point and room name
2. Connects to the entry point
3. Requests the specified room
4. Extracts connection candidates and port
5. Tests connectivity to each candidate
6. Launches the real SSH client to connect directly to the room

## Usage

### Basic Syntax

```bash
./unn-ssh.sh [options] ssh://[user@]entrypoint[:port][/roomname]
```

### Options

- `-identity <path>`: Path to a specific private key for authentication (default: searches `~/.ssh/id_ed25519` and `~/.ssh/id_rsa`).
- `-v`: Verbose output.

### Examples

Connect to a specific room:
```bash
./unn-ssh.sh ssh://localhost:44322/myroom
```

Connect to the entry point interactively (interactive selection mode):
```bash
./unn-ssh.sh ssh://localhost:44322
```

Connect with a specific identity:
```bash
./unn-ssh.sh -identity ~/.ssh/my_unn_key ssh://unn.example.com/lobby
```

## Interactive Mode

If no room name is specified in the URL, `unn-ssh` will:
1. Connect to the entry point.
2. Open an interactive shell (BBS style).
3. Allow you to list rooms (`/rooms`), register keys (`/register`), or simply type a room name to teleport.

## Persistence

The wrapper is designed for seamless navigation:
1. When you "teleport" to a room from the entry point shell, the wrapper handles the P2P connection automatically.
2. When you exit the room (via `/exit` or terminating the session), the wrapper automatically reconnects you to the entry point shell.
3. This allows you to jump between rooms without restarting the wrapper.

## How It Works

1. **URL Parsing**: The wrapper extracts the entry point address, port, username, and room name.
2. **Entry Point Connection**: Connects to the entry point using SSH public key authentication.
3. **Shell/Interaction**: 
   - If a room was specified, it sends it to the entry point.
   - If not, it provides a raw terminal for you to interact with the entry point.
4. **Hole-Punching Discovery**: Monitors the entry point output for a `[CONNECTION_DATA]` block containing P2P candidates and the target's public host keys.
5. **Connectivity Test**: Quickly probes candidates to find a reachable one.
6. **SSH Handoff**: Executes the system `ssh` client with a temporary `known_hosts` file containing the verified host keys, ensuring `StrictHostKeyChecking=yes`.

## Building

The wrapper is automatically built when you run `./unn-ssh.sh`. To build manually:

```bash
go build -o unn-ssh-bin ./cmd/unn-ssh
```

## Requirements

- Go 1.16 or later (for building)
- Standard `ssh` client installed on your system
- Network connectivity to the entry point and room candidates

## Security

- **Authentication**: Uses public key authentication.
- **Host Keys**: Automatically handles room host keys by creating a temporary `known_hosts` file, ensuring you are connecting to the intended room operator.
- **Raw Mode**: Temporarily sets your local terminal to raw mode for a responsive BBS experience, restoring it on exit or room transition.
