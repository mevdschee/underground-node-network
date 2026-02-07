# UNN General Concepts

This section covers the fundamental principles and protocols that underpin the entire Underground Node Network (UNN). These concepts apply across all [UNN Applications](../apps/README.md).

### [Identity & Verification](identity.md)
The UNN uses a decentralized, platform-linked identity system. Users verify their SSH keys against profiles like GitHub or GitLab to build trust and claim unique usernames.

### [P2P & NAT Traversal](p2p-nat.md)
The network operates as a living constellation of personal nodes. We use **SSH over QUIC** with entrypoint-coordinated UDP hole-punching to establish direct connections between users, ensuring the entrypoints never proxy private room traffic.

### [In-Band & Out-of-Band Signaling](signaling.md)
Coordination is handled via SSH subsystems (`unn-control`, `unn-signaling`) for P2P signaling and ANSI OSC 31337 sequences for invisible in-band automation. The `unn-client` is required for room connections.

### [TUI & Room Interaction](tui_and_doors.md)
The visitor experience is built around Terminal User Interfaces (TUI). This doc covers how the Chat UI, Input Bridges, and Doors work together to create a seamless interactive environment.

### [Implementation Details](implementation_details.md)
A technical deep-dive into the security model, handover trust, and the low-level mechanics of the system.
