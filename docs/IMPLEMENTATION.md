# UNN Implementation Details

The UNN is built on standard SSH primitives, using custom subsystems and signaling payloads to create a distributed network mesh.

## Security Model: Handover Trust

Authentication in the UNN is hierarchical:
1. **Entry Point Auth**: Standard SSH public key authentication. Users must manually register via `/register`.
2. **Room Handover**: When a visitor connects to a room, the entry point includes the visitor's authenticated public key in the `punch_offer` signaling payload.
3. **Room Auth**: The room's ephemeral SSH server enforces strict public key authentication, only accepting keys that were pre-authorized by the entry point.

## Connection Lifecycle

1. **Client Startup**: `unn-client` starts a local SSH server. It then connects to the entry point. If this connection fails, the client exits (fatal).
2. **Registration**: The client registers its room, doors, and connection candidates.
3. **Visitor Jump**:
   - Visitor requests a room at the entry point.
   - Entry point signals the visitor's public key to the room operator (`punch_offer`).
   - Room operator registers the key with the local SSH server.
   - Entry point signals room candidates and host keys to the visitor (`punch_start`).
   - Visitor (wrapper) probes candidates and initiates direct SSH connection.
4. **Session**: Visitor enters the room. **Ctrl+C** is managed by the wrapper to allow instant exit back to the entry point shell.

## Network Protocols

### Signaling JSON
All coordination happens over an `unn-control` SSH subsystem using JSON messages.
- `register`: Room metadata, candidates, and host keys.
- `punch_offer`: Visitor ID, candidates, and **VisitorKey** (captured by entry point).
- `punch_answer`: Operator candidates and SSH port.
- `punch_start`: Final sync message to trigger hole-punching.

### Stdin Management
To ensure a smooth "teleport" experience, the `unn-ssh` wrapper implements an asynchronous stdin manager. This manager can be paused during handle-off to the system SSH client, preventing the wrapper from "eating" characters intended for the room session.
