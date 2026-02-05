# UNN Implementation Details (Technical)

This document provides a deep dive into the technical architecture and security model of the network.

### Security Model: Handover Trust
Authentication in UNN is hierarchical and relies on a "handover" of trust from the entrypoint to the room node:
1. **Entrypoint Auth**: Standard SSH public key authentication.
2. **Identity Handover**: When a visitor "teleports," the entrypoint signals the visitor's verified public key and platform-username to the room node.
3. **Room Auth**: The room node's SSH server enforces strict public key authentication, only accepting keys that match those pre-authorized by the signaling hub. The room node's identity is defined by its **SSH Host Key**. 

### Managed I/O bridging
To ensure a smooth transition between the Chat UI and external "Doors," we implement a custom I/O bridging layer:
- **`InputBridge`**: An asynchronous "pump" that reads raw bytes from the SSH channel and distributes them to the active consumer.
- **`SSHBus`**: A specialized implementation of `tcell.Tty` that consumes from the bridge. It prioritizes out-of-band signals (like door exit) to interrupt blocked reads immediately.

### Zmodem-style OSC Block Transfers
The file transfer system is a **Zmodem-style, integrated in-band protocol**. It uses the existing SSH channel to stream file data without needing secondary listeners:
1. **Request**: User triggers a download (e.g., via the `/files` door).
2. **Segmentation**: The room server reads the file in 8KB blocks.
3. **Encoding**: Each block is Base64 encoded and wrapped in an OSC 31337 JSON sequence (`transfer_block`) containing a session UUID, block index, total count, and a SHA256 file checksum.
4. **Rate Limiting**: Small delays are introduced between blocks based on the node's `uploadLimit` to prevent saturating the interactive connection.
5. **Client-side Reception**: The `unn-client` intercepts these sequences, appends them to a persistent `.parts` file (stored as NDJSON), and reassembles the final file once all blocks have arrived. Integrity is verified via SHA256 before the file is finalized.

---
See the [Application Overview](../apps/README.md) for individual component details.
