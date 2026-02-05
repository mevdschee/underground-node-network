# UNN Room Node (`unn-room`)

The **UNN Room Node** is a user-hosted application that creates a personal "room" on the network. It combines a secure SSH server with a flexible application hosting environment ("Doors").

### Role & Responsibilities
- **Hosting**: Serves an ephemeral SSH environment for visitors.
- **Door Management**: Executes and manages local applications (Doors) for visitors.
- **File Sharing**: Streams files in cryptographically verified blocks via OSC signaling.
- **P2P Server**: Handles direct incoming connections authorized by the entrypoint.

### Persistence & Identity
A Room Node identifies itself on the network using its **Host Key**:
- **Room Host Key**: Defined by `~/.unn/room_host_key`. This is the room's cryptographic identity on the entrypoint and what visitors use to verify the server.
- **Owner-First Identity**: The room node primarily connects to the entrypoint using your **Owner Identity** (from `~/.ssh/id_rsa` or similar). This ensures the room is always linked to your verified platform account.
- **Identity Stability**: While the connection uses your identity, the room still has a **Host Key** (`~/.unn/room_host_key`). This key is what visitors verify when they connect directly to your node.
- **Auto-Registration**: Registration is **silent**. When you launch `unn-room` with a free name, the entrypoint automatically claims it for you and authorizes your current host key.
- **Automatic Key Rotation**: If you rotate your host key, the entrypoint will automatically detect that you are the owner (via your personal identity) and trust the new key.
- **Name Protection**: Once a name is claimed, it is locked to your account. No other user can hijacked your room name, even if they have your host key (because they lack your personal identity key).
- **Last Seen**: The entrypoint tracks the "last seen" date of both users and rooms to maintain an active registry.

### Key Topics
- [Hosting Doors](../concepts/tui_and_doors.md#doors) - How to add interactive programs to your room.
- **Files Door**: A standalone Go application (`doors/files/main.go`) that provides a menu for browsing and downloading files via OSC.
- [Secure File Downloads](../concepts/signaling.md#osc-31337-block-transfers-zmodem-like) - Block-based transfers with SHA256 integrity.
- [P2P Authentication](../concepts/identity.md#room-auth) - How rooms verify visitor keys without a central proxy.
- [Chat & Interaction](../concepts/tui_and_doors.md#chat) - The built-in BBS chat experience.

---
For technical setup, see the [Implementation Details](../concepts/implementation_details.md).
