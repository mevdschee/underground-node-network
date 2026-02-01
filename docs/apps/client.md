# UNN Client (`unn-client`)

The **UNN Client** is the primary client-side tool for navigating the network. It automates the complex signaling and connection logic required to jump between the entrypoint and room nodes.

### Usage
The client is invoked using the `unn://` scheme:
```bash
./unn-client.sh unn://<entrypoint server>/[room name]
```
- **Entrypoint**: The address of the signaling hub (defaults to port **44322**).
- **Room Name**: (Optional) If provided, the client will immediately attempt to join that room. If omitted, the client starts in interactive mode.
- **Downloads**: Use `-downloads <path>` to specify where files are saved (defaults to `~/Downloads`).

### Zmodem-style File Transfers
The client implements a resilient, **Zmodem-like block-based transfer mechanism**:
- **In-band streaming**: Files are sent directly over the active SSH terminal using hidden OSC signals.
- **Resilient Reassembly**: Blocks are stored as NDJSON in `.parts` files, allowing for future completion of interrupted transfers.
- **Collision Avoidance**: If a file already exists in the download directory, the client automatically appends a number (e.g., `file (1).ext`) to prevent overwriting data.
- **Integrity**: Each transfer is verified with a SHA256 checksum after reassembly.

### Role & Responsibilities
- **Teleportation**: Monitors entrypoint output for signaling and automatically initiates room connections.
- **NAT Probing**: Tests and selects the best connection candidates (Local, Public, or Tunnel).
- **Automation**: Handles automated file downloads and terminal state management during jumps.
- **Persistence**: Keeps the session alive and returns the user to the entrypoint when a room connection ends.

### Key Topics
- [OSC Signaling](../concepts/signaling.md#osc-9) - The invisible communication layer used for automation.
- [Jump Logic](../concepts/p2p-nat.md#probe-and-select) - How the client picks the fastest path to a node.
- [Managed I/O](../concepts/tui_and_doors.md#stdin-bridge) - How Ctrl+C and window resizing are preserved across connections.

---
For technical architecture, see [Client Architecture](../architecture/client.md).
