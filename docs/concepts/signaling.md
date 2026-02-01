# In-Band & Out-of-Band Signaling

The network uses two distinct layers of communication to keep everything in sync.

### 1. Control Subsystem (Out-of-Band)
The `unn-control` SSH subsystem is used for high-level coordination between the entrypoint and room nodes.
- **Protocol**: JSON messages.
- **Usage**: Room registration, P2P candidate exchange, and identity handover.

### 2. OSC 31337 Sequences (In-Band)
To provide a seamless experience for visitors, we use invisible **ANSI OSC 31337** sequences. These sequences are embedded in the normal terminal output stream and captured by the `unn-client` tool.

- **Format**: `\x1b]31337;{"action":"...","...":"..."}\x07`
- **Actions**:
    - `teleport`: Moves the user from the entrypoint to a direct room connection.
    - `transfer_block`: Replaces the legacy `download` action. Streams file data in 8KB blocks.
    - `popup`: Shows a stylized terminal-resident notification box.

### OSC 31337 Block Transfers (Zmodem-like)
To avoid opening additional ports or requiring secondary SSH channels, UNN uses an **in-band, Zmodem-like block transfer protocol**. This allows files to be streamed directly over the existing interactive session:
1. **Segmentation**: The server reads the file in 8192-byte chunks.
2. **Encoding & Framing**: Each chunk is **Base64 encoded** and wrapped in an OSC 31337 JSON payload (`transfer_block`).
3. **Transmission**: The payloads are printed to the server's stdout, where they are captured by the `unn-client`.
4. **Res resilient Storage**: The client stores blocks as **NDJSON (Newline Delimited JSON)** in a `.parts` file. This ensures that even if a transfer is interrupted, the received data is preserved.
5. **Reassembly & Integrity**: Once the last block (index == total-1) is received, the client reassembles the file and verifies it against a SHA256 checksum provided in the first block's metadata.
6. **Rate Limiting**: The server can introduce small delays between blocks to stay within configured upload limits without affecting terminal responsiveness.

### Why OSC?
Using OSC allows the servers to control the client tool without needing a separate network port or a custom protocol. It works over any standard SSH terminal, though only the `unn-client` is "aware" enough to act on the signals.

## Message Parameter Reference

Detailed parameter documentation is split into separate files for clarity:

- [**Out-of-Band (Control Subsystem)**](signaling_oob.md) - Messages between nodes for coordination.
- [**In-Band (ANSI OSC 31337)**](signaling_inband.md) - Signals embedded in terminal output for client automation.

---
See the [UNN Client Usage](../apps/client.md#usage) for details on the `unn://` scheme.
