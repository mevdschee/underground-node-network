# UNN Implementation Details (Technical)

This document provides a deep dive into the technical architecture and security model of the network.

### Security Model: Handover Trust
Authentication in UNN is hierarchical and relies on a "handover" of trust from the entrypoint to the room node:
1. **Entrypoint Auth**: Standard SSH public key authentication.
2. **Identity Handover**: When a visitor "teleports," the entrypoint signals the visitor's verified public key and platform-username to the room node.
3. **Room Auth**: The room node's ephemeral SSH server enforces strict public key authentication, only accepting keys that match those pre-authorized by the signaling hub.

### Managed I/O bridging
To ensure a smooth transition between the Chat UI and external "Doors," we implement a custom I/O bridging layer:
- **`InputBridge`**: An asynchronous "pump" that reads raw bytes from the SSH channel and distributes them to the active consumer.
- **`SSHBus`**: A specialized implementation of `tcell.Tty` that consumes from the bridge. It prioritizes out-of-band signals (like door exit) to interrupt blocked reads immediately.

### One-Shot SFTP & Filename Obfuscation
The file transfer system is designed to be invisible to protocol sniffers:
1. **Request**: User triggers `/get filename`.
2. **Ephemeral Server**: Room server spawns a temporary SSH listener on a random port.
3. **Signaling**: Client receives an OSC 9 message with a random **UUIDv4** and the ephemeral port.
4. **Transfer**: Client connects to the ephemeral port and requests the UUID (not the original filename).
5. **Mapping**: The server maps the UUID back to the real file on disk.

---
See the [Application Overview](../apps/README.md) for individual component details.
