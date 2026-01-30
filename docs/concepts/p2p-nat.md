# P2P & NAT Traversal

UNN is a living mesh of user-hosted nodes. Because most users are behind NAT (Network Address Translation), a direct connection is not always trivial.

### The Signaling Flow
1. **Advertising**: Room nodes register multiple "Candidates" (Public IP, LAN IP, Reverse Tunnel ports) with the entrypoint.
2. **Offer**: When a visitor requests a room, the entrypoint notifies the room operator.
3. **Answer**: The operator sends back the most up-to-date connection parameters.
4. **Hole-Punching**: Both peers attempt to "punch" through their firewalls simultaneously to establish a direct path.

### Probe and Select
The **UNN SSH Wrapper** (`unn-ssh`) automates the selection process:
- It probes all candidates in parallel.
- It selects the fastest reachable path (favoring direct LAN or public IP over tunnels).
- It initiates the SSH handshake immediately upon discovery.

### Reusable Tunneling
For users in restrictive environments, UNN supports reverse SSH tunneling. The room server maintains a persistent connection to the entrypoint, which acts as a static jump-point if direct hole-punching fails.

---
See also: [Trust Handover](implementation_details.md#security-model-handover-trust)
