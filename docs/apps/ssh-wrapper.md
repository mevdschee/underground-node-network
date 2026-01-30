# UNN SSH Wrapper (`unn-ssh`)

The **UNN SSH Wrapper** is the primary client-side tool for navigating the network. It automates the complex signaling and connection logic required to jump between the entrypoint and room nodes.

### Role & Responsibilities
- **Teleportation**: Monitors entrypoint output for signaling and automatically initiates room connections.
- **NAT Probing**: Tests and selects the best connection candidates (Local, Public, or Tunnel).
- **Automation**: Handles automated file downloads and terminal state management during jumps.
- **Persistence**: Keeps the session alive and returns the user to the entrypoint when a room connection ends.

### Key Topics
- [OSC Signaling](../concepts/signaling.md#osc-9) - The invisible communication layer used for automation.
- [Jump Logic](../concepts/p2p-nat.md#probe-and-select) - How the wrapper picks the fastest path to a node.
- [Managed I/O](../concepts/tui_and_doors.md#stdin-bridge) - How Ctrl+C and window resizing are preserved across connections.

---
For usage instructions, see [SSH_WRAPPER.md](../../docs/SSH_WRAPPER.md).
