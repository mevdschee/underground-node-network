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
- **Owner Identity**: While the room name is registered by an owner (using their SSH key), the room node *itself* connects using its host key to prove it is the authorized server for that name.
- **Identity Stability**: Storing the host key locally ensures that your room's identity remains stable across restarts. Visitors' SSH clients will not trigger "Host Key Changed" warnings.

- **Manual Registration**: Before a room can connect, the owner must manual register it within the entrypoint UI using the command:
  ```bash
  /register <roomname> <host_key_hash>
  ```
  You can find your host key hash in the logs when starting `unn-room` (or by hashing the public key).
- **Name Ownership**: Once registered, the entrypoint locks that name to your **Owner Identity**. Other users cannot hijack your room name. 
- **Last Seen**: The entrypoint tracks the "last seen" date of both users and rooms to maintain an active registry.

### Key Topics
- [Hosting Doors](../concepts/tui_and_doors.md#doors) - How to add interactive programs to your room.
- **Files Door**: A standalone Go application (`doors/files/main.go`) that provides a menu for browsing and downloading files via OSC.
- [Secure File Downloads](../concepts/signaling.md#osc-31337-block-transfers-zmodem-like) - Block-based transfers with SHA256 integrity.
- [P2P Authentication](../concepts/identity.md#room-auth) - How rooms verify visitor keys without a central proxy.
- [Chat & Interaction](../concepts/tui_and_doors.md#chat) - The built-in BBS chat experience.

---
For technical setup, see the [Implementation Details](../concepts/implementation_details.md).
