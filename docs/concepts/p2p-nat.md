# P2P & NAT Traversal

UNN is a living mesh of user-hosted nodes. Because most users are behind NAT (Network Address Translation), a direct connection is not always trivial. UNN uses **QUIC over UDP** for P2P room connections, with **SSH running over QUIC streams**. This provides reliable, encrypted transport with built-in NAT traversal capabilities.

### The Signaling Flow
1. **Candidate Discovery**: Room nodes discover their local interface candidates and receive their **server-reflexive address** from the entrypoint (the public IP:port as seen by the TCP SSH connection). No external STUN servers are used.
2. **Registration**: Candidates are registered with the entrypoint via the `unn-signaling` SSH subsystem (over TCP).
3. **Coordinated Hole-Punching**: When a visitor requests a room, the entrypoint **orchestrates two-way UDP hole-punching**. Both client and room begin punching simultaneously via the `unn-control` subsystem.
4. **QUIC Connection**: Once holes are established, a QUIC connection is created over UDP. The room opens a listener, and the client connects.
5. **SSH over QUIC**: An SSH session is established over a QUIC stream, providing the interactive terminal connection.

### Connection Types
- **Entrypoint connection**: Traditional **TCP SSH** (port 44322) for the lobby, signaling, and coordination.
- **Room connection**: **SSH over QUIC (UDP)** for direct P2P connections to rooms.

### Probe and Select
The **UNN Client** (`unn-client`) automates the connection process:
- It registers its candidates with the entrypoint via SSH signaling.
- It requests coordinated hole-punching via the `unn-control` subsystem.
- It establishes a QUIC connection to the room and opens an SSH session over a QUIC stream.

### Reusable Tunneling
For users in restrictive environments where UDP is blocked, UNN supports reverse SSH tunneling over TCP. The room server maintains a persistent connection to the entrypoint, which acts as a fallback if direct QUIC connections fail.

---
See also: [Trust Handover](implementation_details.md#security-model-handover-trust)

