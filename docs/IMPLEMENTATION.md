# UNN Implementation Details

The UNN is built on standard SSH primitives, using custom subsystems and signaling payloads to create a distributed network mesh.

## Security Model: Handover Trust

Authentication in the UNN is hierarchical:
1. **Entry Point Auth**: Standard SSH public key authentication. Users must manually register via `/register`.
2. **Room Handover**: When a person connects to a room, the entry point includes the person's authenticated public key in the `punch_offer` signaling payload.
3. **Room Auth**: The room's ephemeral SSH server enforces strict public key authentication, only accepting keys that were pre-authorized by the entry point.

## Connection Lifecycle

1. **Client Startup**: `unn-client` starts a local SSH server. It then connects to the entry point. If this connection fails, the client exits (fatal).
2. **Registration**: The client registers its room, doors, and connection candidates.
3. **Person Jump**:
   - Person requests a room at the entry point.
   - Entry point signals the person's public key to the room operator (`punch_offer`).
   - Room operator registers the key with the local SSH server.
   - Entry point signals room candidates and host keys to the person (`punch_start`).
   - Person (wrapper) probes candidates and initiates direct SSH connection.
4. **Session**: Person enters the room. **Ctrl+C** is managed by the wrapper to allow instant exit back to the entry point shell.

## Network Protocols

### Signaling JSON
All coordination happens over an `unn-control` SSH subsystem using JSON messages.
- `register`: Room metadata, candidates, and host keys.
- `punch_offer`: Person ID, candidates, and **PersonKey** (captured by entry point).
- `punch_answer`: Operator candidates and SSH port.
- `punch_start`: Final sync message to trigger hole-punching.

### Stdin Management & Signaling
To ensure a smooth transition between the Chat UI and external "Doors," the UNN implement its own I/O bridging layer:

1. **InputBridge**: An asynchronous "pump" that reads raw bytes from the SSH channel and distributes them to a single active consumer via a Go channel. This decouples the network read from the TUI's internal event loop.
2. **SSHBus**: A specialized implementation of `tcell.Tty` that consumes from the `InputBridge`.
   - **Prioritized Signaling**: It checks for an internal `doneChan` signal before every read operation. This allows the system to interrupt a blocked read the exact moment a door exits or the TUI is suspended.
   - **Reset Capability**: The bus can be reset between transitions, clearing old "stop" signals and allowing consecutive programs (e.g., exiting one door and entering another) to receive a fresh input stream.

### Asynchronous Redraws
The Chat UI implements a decoupled drawing mechanism. UI updates (messages, person list changes) are pushed to a dedicated `drawChan`, which triggers a screen refresh independently of the TUI's event loop. This prevents the UI from "freezing" when waiting for user input.

## Advanced Features

### One-Shot SFTP Server & Obfuscated Transfer

UNN implements a highly secure file transfer mechanism designed to be resilient against protocol analysis and unauthorized metadata collection:

1. **Orchestration**: When a person types `/download <file>`, the room server generates a random **UUIDv4** and starts an internal one-shot SSH server on a random port.
2. **Signaling**: The room server outputs a hidden `[DOWNLOAD FILE]` block containing the original filename, the one-shot port, and the transfer UUID.
3. **Mutual Auth**: The one-shot server is configured to *only* accept the authenticated public key of that specific person. Both sides verify host keys to ensure a trusted channel.
4. **Obfuscation**: During the SFTP session, the client *only* requests the UUID. The server's SFTP handler (from `internal/fileserver`) maps this UUID back to the real file on disk. Any protocol sniffer will only see a request for a random UUID string, never the actual filename or path.
6. **Graceful Teardown**: The one-shot server signals success with an `exit-status` of 0. It then waits a brief period (100ms) to ensure the client receives all termination packets before closing the local listener.
7. **Connection Persistence**: The main room connection remains open until the person explicitly disconnects. This ensures that the wrapper has sufficient time to complete the SFTP transfer through the existing tunnel.
8. **Automatic Cleanup**: The one-shot SFTP server shuts down immediately after the transfer completes or after a configurable timeout (default 60s).

### Chat History Persistence & Security

The room server maintains an in-memory history to improve the experience of reconnections (e.g., after a file download or a network hiccup):

1. **Identifier**: History is tracked per **person Public Key** (using a SHA256 hash).
2. **Connection-Only Logging**: To ensure security, messages are only appended to a user's history if they are **currently connected** to the room. Users can never "back-read" messages that were sent when they were offline.
3. **Replay**: When a user reconnects, the server automatically replays their private history (up to 200 messages) to their TUI.
4. **Volatility**: History is stored in-memory and is wiped if the room node (client) is restarted.
5. **Wipe Command**: Users can manually purge their history and clear their screen at any time using the `/clear` command.
