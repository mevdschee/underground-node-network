# UNN Download Tool (`unn-dl`)

The **UNN Download Tool** is a specialized, one-shot SFTP client designed to work in tandem with the [Room Node](room.md) and [SSH Wrapper](ssh-wrapper.md).

### Role & Responsibilities
- **Secure Retrieval**: Connects to ephemeral one-shot SFTP servers.
- **Verification**: Enforces strict host key verification and mutually authorizes the transfer.
- **Automation**: Receives transfer parameters (UUID, port, signature) via the wrapper for fully silent operations.

### Key Logic
- Works by requesting a unique **UUIDv4** instead of a filename to obfuscate activity.
- Exits immediately after a single file is successfully transferred.
- Integrated into the wrapper for a "one-click" download feel from within the room chat.
