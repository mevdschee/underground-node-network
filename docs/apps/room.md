# UNN Room Node (`unn-room`)

The **UNN Room Node** is a user-hosted application that creates a personal "room" on the network. It combines a secure SSH server with a flexible application hosting environment ("Doors").

### Role & Responsibilities
- **Hosting**: Serves an ephemeral SSH environment for visitors.
- **Door Management**: Executes and manages local applications (Doors) for visitors.
- **File Sharing**: Provides secure, one-shot SFTP servers for authenticated file downloads.
- **P2P Server**: Handles direct incoming connections authorized by the entrypoint.

### Key Topics
- [Hosting Doors](../concepts/tui_and_doors.md#doors) - How to add interactive programs to your room.
- [Secure File Downloads](../concepts/file_transfers.md) - UUID-obfuscated transfers and SFTP isolation.
- [P2P Authentication](../concepts/identity.md#room-auth) - How rooms verify visitor keys without a central proxy.
- [Chat & Interaction](../concepts/tui_and_doors.md#chat) - The built-in BBS chat experience.

---
For technical setup, see the [Implementation Details](../concepts/implementation_details.md).
