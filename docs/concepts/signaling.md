# In-Band & Out-of-Band Signaling

The network uses two distinct layers of communication to keep everything in sync.

### 1. Control Subsystem (Out-of-Band)
The `unn-control` SSH subsystem is used for high-level coordination between the entrypoint and room nodes.
- **Protocol**: JSON messages.
- **Usage**: Room registration, P2P candidate exchange, and identity handover.

### 2. OSC 9 Sequences (In-Band)
To provide a seamless experience for visitors, we use invisible **ANSI OSC 9** sequences. These sequences are embedded in the normal terminal output stream and captured by the `unn-client` tool.

- **Format**: `\x1b]9;{"action":"...","...":"..."}\x07`
- **Actions**:
    - `teleport`: Moves the user from the entrypoint to a direct room connection.
    - `transfer_block`: Replaces the legacy `download` action. Streams file data in 8KB blocks.
    - `popup`: Shows a stylized terminal-resident notification box.

### OSC 9 Block Transfers
To avoid opening additional ports for file transfers (like SFTP did), UNN uses **in-band block transfers**. When a user requests a file:
1. The server reads the file in 8192-byte chunks.
2. Each chunk is **Base64 encoded** and wrapped in an OSC 9 JSON payload.
3. The client captures these sequences and stores them as **NDJSON (Newline Delimited JSON)** in a `.parts` file (e.g., `filename.ext.<uuid>.parts`) within the download directory.
4. **Reassembly**: Once all blocks (identified by index and total count) are received, the client reassembles the final file.
5. **Integrity**: Each block belongs to a transfer session identified by a UUID, and the client verifies the final file against a SHA256 checksum sent by the server.
6. **Rate Limiting**: The server can introduce small delays between blocks to stay within configured upload limits.

### Why OSC?
Using OSC allows the servers to control the client tool without needing a separate network port or a custom protocol. It works over any standard SSH terminal, though only the `unn-client` is "aware" enough to act on the signals.

---
See the [UNN Client Usage](../apps/client.md#usage) for details on the `unn://` scheme.
