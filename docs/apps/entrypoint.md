# UNN Entrypoint (`unn-entrypoint`)

The **UNN Entrypoint** is the signaling hub and discovery backbone of the Underground Node Network. It acts as a rendezvous point where room nodes and human visitors meet to coordinate peer-to-peer connections.

### Role & Responsibilities
- **Signaling**: Facilitates P2P handshakes (hole-punching) between visitors and room nodes.
- **Rendezvous**: Maintains a real-time directory of active room nodes.
- **Identity**: Verifies user public keys against external platforms (GitHub, etc.) and manages the registration database.
- **BBS Interface**: Provides the terminal-based landing experience for manual SSH users.

### Key Topics
- [Public Key Registration](../concepts/identity.md#registration) - How users claim their identity.
- [Hole-Punching Coordination](../concepts/p2p-nat.md#signaling-flow) - The flow of candidates between peers.
- [User Onboarding](../concepts/identity.md#onboarding) - The process of linking keys to external profiles.

---
For more details, see the [Implementation Notes](../concepts/implementation_details.md).
