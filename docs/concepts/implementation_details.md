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

### OSC-based Block Transfers
The file transfer system is designed to be fully integrated into the existing SSH channel, avoiding the need for ephemeral ports or secondary listeners:
1. **Request**: User triggers `/get filename`.
2. **Segmentation**: The room server reads the file in 8KB blocks.
3. **Encoding & Framing**: Each block is Base64 encoded and wrapped in an OSC 9 JSON sequence containing the transfer session UUID, block index, total count, and a SHA256 file checksum.
4. **Rate Limiting**: The server introduces a calculated delay between blocks based on the node's `uploadLimit` to ensure background transfers don't saturate the connection.
5. **Reassembly & Verification**: The client reassembles the blocks in a temporary file and verifies the final SHA256 checksum before moving it to the local Downloads directory.

---
See the [Application Overview](../apps/README.md) for individual component details.
