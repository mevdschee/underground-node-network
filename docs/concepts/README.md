# UNN General Concepts

This section covers the fundamental principles and protocols that underpin the entire Underground Node Network (UNN). These concepts apply across all [UNN Applications](../apps/README.md).

### [Identity & Verification](identity.md)
The UNN uses a decentralized, platform-linked identity system. Users verify their SSH keys against profiles like GitHub or GitLab to build trust and claim unique usernames.

### [P2P & NAT Traversal](p2p-nat.md)
The network operates as a living constellation of personal nodes. We use P2P techniques (hole-punching and reverse tunnels) to establish direct SSH connections between users, ensuring the entrypoints never proxy private room traffic.

### [In-Band & Out-of-Band Signaling](signaling.md)
All coordination is handled via standard SSH primitives. We use custom subsystems for control messages and ANSI OSC 31337 sequences for invisible in-band signaling to automate the user experience.

### [TUI & Room Interaction](tui_and_doors.md)
The visitor experience is built around Terminal User Interfaces (TUI). This doc covers how the Chat UI, Input Bridges, and Doors work together to create a seamless interactive environment.

### [Implementation Details](implementation_details.md)
A technical deep-dive into the security model, handover trust, and the low-level mechanics of the system.
