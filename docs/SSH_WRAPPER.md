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
./unn-ssh.sh ssh://[user@]entrypoint[:port]/roomname
```

### Examples

Connect to a room on localhost:
```bash
./unn-ssh.sh ssh://localhost:44322/myroom
```

Connect with a specific username:
```bash
./unn-ssh.sh ssh://alice@unn.example.com/hackerspace
```

Connect to a room on a remote entry point:
```bash
./unn-ssh.sh ssh://entry.unn.network:2222/cybercafe
```

### Verbose Mode

Use the `-v` flag to see detailed connection progress:

```bash
./unn-ssh.sh -v ssh://localhost:44322/myroom
```

This will show:
- Entry point connection status
- Room request details
- Candidate discovery
- Connection attempts
- SSH client launch

## How It Works

1. **URL Parsing**: The wrapper extracts the entry point address, port, username, and room name from the URL
2. **Entry Point Connection**: Connects to the entry point using SSH
3. **Room Request**: Sends the room name to the entry point's interactive menu
4. **Response Parsing**: Extracts the candidate IPs and SSH port from the entry point's response
5. **Connectivity Test**: Tests each candidate to find a reachable one
6. **SSH Handoff**: Executes the standard `ssh` command to connect to the room

## Integration with Entry Point

When you connect to a room through the entry point's interactive menu, it now displays both:

1. **Wrapper URL**: An `ssh://` link you can use with `unn-ssh`
2. **Direct Commands**: Traditional `ssh -p` commands for manual connection

Example output:
```
[Connection ready!]

Connect using unn-ssh wrapper:
  ssh://0.0.0.0:44322/myroom

Or connect directly:
  ssh -p 2222 192.168.1.100
  ssh -p 2222 10.0.0.5
```

## Building

The wrapper is automatically built when you run `./unn-ssh.sh`. To build manually:

```bash
go build -o unn-ssh-bin ./cmd/unn-ssh
```

## Requirements

- Go 1.16 or later (for building)
- Standard `ssh` client installed on your system
- Network connectivity to the entry point and room candidates

## Limitations

- Currently uses `ssh.InsecureIgnoreHostKey()` - host key verification should be added for production use
- Requires the standard `ssh` command to be available in PATH
- Timeout is fixed at 30 seconds for room response
- Only tests TCP connectivity, not full SSH handshake, before launching client
