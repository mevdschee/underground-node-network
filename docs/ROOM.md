Project Brief: The UNN Room Node

The UNN Room node is the doorway into your own personal space inside the Underground Network. When you start the room server, it spins up an ephemeral SSH server bound locally (e.g., 127.0.0.1:2222) and registers with an entry point. The entry point helps coordinate NAT traversal—advertising your reachable candidates (public IP guess, LAN IP, reverse tunnel port) and orchestrating hole-punching so people can connect directly to your node over SSH. Once the P2P connection is established, the entry point is out of the picture. The server also opens a second, hidden channel — a control line (via a subtle status bar at the bottom of the terminal) that turns your SSH session into a control console. In the window you can see the normal SSH session and visit the network as you would normally.

Through this side‑channel, the room announces the doors you host: small executable programs in a local directory, written in any language, each one a tiny world or tool you've chosen to expose. People who enter your node can run these doors, and every invocation flows back through the room server. You see who’s knocking, what they’re running, and whether you want to allow it.

When approval is needed, a stark yes/no prompt, a warning, a request for approval is shown. It feels like a system interrupt, a pulse from the underground.

A slim operator bar sits at the edge of your terminal, showing active people, running doors, and signals from the network. It’s subtle, but alive — a heartbeat of your node.

The UNN Room Node doesn’t change SSH. It simply adds a hidden layer beneath it, giving every user the power to operate a node, host services, and shape their corner of the network. It’s the control panel for your room in the underground.

When you are connected you can idle in your room. The room server provides an interactive BBS experience.

See [UNN Chat](CHAT.md) for full details on chat features, commands, and security rules.
- **Untangled SFTP**: The room server uses a **one-shot SFTP server** model. For every download request, a separate SSH server is spawned on a random port. This server is "jailed" to serve exactly one file and enforces **mutual authentication** (verifying your key against the one used to join the room).
- **Filename Obfuscation**: To prevent protocol analysis from capturing shared filenames, the room server generates a random **UUIDv4** for the transfer. The actual filename is never sent during the SFTP session; the wrapper automatically resolves the UUID back to the original name upon local storage.
- **Configurable Window**: You can configure the download window (default 60s) using the `-timeout` flag on the room node.

### Snappy Interactive Logic
The room handles transitions and UI redraws with specialized low-level signaling to ensure zero-lag interactions and secure history persistence. Detailed mechanics are documented in [UNN Chat](CHAT.md).

The room node has two types of doors: 
1. Applications: These are doors that are run locally on the room server. When a user enters your room they can start an application, the applications has an entry in the room chat (prefixed with slash) and shows the number of people that are currently using the application. The application can be started by typing /appname. The application starts fullscreen and can be interrupted by **Ctrl+C**. 
2. Agents: These are chat bots that have a presence in the chat of the room. They have a name and can react to anything. You can address them by typing @agentname followed by a message. The agent will then respond to the message. 

## Authentication

### User Identity
When connecting to the entry point, the room node uses a **User Key** to authenticate.
- **Source**: It automatically detects your system key (`~/.ssh/id_ed25519` or `~/.ssh/id_rsa`).
- **Registration**: Before starting the room node, you must register your public key manually with the entry point (via `/register`). Registration is **strictly enforced**—the room node will exit with a fatal error if connection or registration fails.

### Person Identity (P2P Auth)
- **Person Verification**: The room server no longer allows anonymous connections. Every person must be pre-authorized by the entry point.
- **Handover**: The entry point signals the person's authenticated public key to your room node, which then pre-authorizes that specific key for the P2P connection.
- **Strict Verification**: Any attempt to connect directly to the room using an unauthorized key will be rejected immediately.

### Room Identity
- **Host Key**: The room node generates a temporary, **ephemeral** SSH host key for your room server.
- **Validity**: This key is valid only as long as the room session exists.
- **Verification**: People verify this ephemeral key via the secure handshake coordinated by the entry point (automated by `unn-ssh` or via manual fingerprint check).