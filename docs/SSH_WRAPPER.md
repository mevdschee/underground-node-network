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

## Interactive Mode

If no room name is specified in the URL, `unn-ssh` will provide a raw terminal for you to interact with the entry point BBS. You can list rooms (`/rooms`), register keys (`/register`), or simply type a room name to teleport.

## Persistence

The wrapper is designed for seamless navigation:
1. When you "teleport" to a room, the wrapper handles the P2P transition automatically.
2. When you exit a room (via **Ctrl+C**), you are automatically returned to the entry point shell.
3. This allows you to jump between rooms without restarting the wrapper.
4. During automatic downloads, the connection is kept alive to allow the SFTP-over-tunnel transfer to finish seamlessly before you return to the entry point.

## Security & Identity

- **Handover**: The wrapper uses the same identity (SSH key) for both the entry point and the room.
- **Strict Verification**: It extract host keys from the entry point signaling and enforces `StrictHostKeyChecking=yes` automatically.
- **P2P Auth**: Only people authenticated by the entry point can complete the P2P connection to a room.

## Technical Features

- **Window Resizing**: Supports `SIGWINCH` for correct PTY sizing.
- **Managed Input**: Uses a managed stdin proxy to ensure `Ctrl+C` behavior is consistent and no characters are lost during transitions.
- **Native Implementation**: Uses the native Go SSH library for the wrapper logic, falling back to system `ssh` only for the final room session.
- **Automated Downloads**: Monitors room output for secure `[GET FILE]` signals. When triggered, the wrapper:
    1. Extracts the one-shot port and transfer UUID.
    2. Establishes a second SSH connection to the room's one-shot server using your existing identity.
    3. Verifies the room's host key (using the signature displayed by the room server).
    4. Downloads the file using the obfuscated UUID to prevent protocol analysis.

### Triggering a Download
Inside a room, you can trigger a file download using the `/get` command:
1. Type `/get filename` (e.g., `/get large_test.bin`).
    5. Saves the file to `~/Downloads` using its original filename, with smart conflict resolution.
    6. Displays the **hex-encoded SHA256** file signature for local manual verification with `sha256sum`.
