# UNN Room Node (`unn-room`)

The **UNN Room Node** is a user-hosted application that creates a personal "room" on the network. It combines a secure SSH server with a flexible application hosting environment ("Doors").

### Role & Responsibilities
- **Hosting**: Serves an ephemeral SSH environment for visitors.
- **Door Management**: Executes and manages local applications (Doors) for visitors.
- **File Sharing**: Streams files in cryptographically verified blocks via OSC signaling.
- **P2P Server**: Handles direct incoming connections authorized by the entrypoint.

### Key Topics
- [Hosting Doors](../concepts/tui_and_doors.md#doors) - How to add interactive programs to your room.
- **Files Door**: A standalone Go application (`doors/files/main.go`) that provides a menu for browsing and downloading files via OSC.
- [Secure File Downloads](../concepts/signaling.md#osc-9-block-transfers) - Block-based transfers with SHA256 integrity.
- [P2P Authentication](../concepts/identity.md#room-auth) - How rooms verify visitor keys without a central proxy.
- [Chat & Interaction](../concepts/tui_and_doors.md#chat) - The built-in BBS chat experience.

---
For technical setup, see the [Implementation Details](../concepts/implementation_details.md).
