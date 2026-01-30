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
    - `download`: Triggers an automated SFTP transfer via `unn-dl`.
    - `popup`: Shows a stylized terminal-resident notification box.

### Why OSC?
Using OSC allows the servers to control the client tool without needing a separate network port or a custom protocol. It works over any standard SSH terminal, though only the `unn-client` is "aware" enough to act on the signals.

---
See the [OSC Protocol Details](../apps/client.md) for details.
