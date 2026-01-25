Project Brief: The UNN Room Client

The UNN Room client is the doorway into your own personal node inside the Underground Network. When you start the client, it spins up an ephemeral SSH server bound locally (e.g., 127.0.0.1:2222) and registers with an entry point. The entry point helps coordinate NAT traversal—advertising your reachable candidates (public IP guess, LAN IP, reverse tunnel port) and orchestrating hole-punching so visitors can connect directly to your node over SSH. Once the P2P connection is established, the entry point is out of the picture. The client also opens a second, hidden channel — a control line (via a subtle status bar at the bottom of the terminal) that turns your SSH session into a control console. In the window you can see the normal SSH session and visit the network as you would normally.

Through this side‑channel, the client announces the doors you host: small executable programs in a local directory, written in any language, each one a tiny world or tool you've chosen to expose. Visitors who enter your node can run these doors, and every invocation flows back through the client. You see who’s knocking, what they’re running, and whether you want to allow it.

When approval is needed, a stark yes/no prompt, a warning, a request for approval is shown. It feels like a system interrupt, a pulse from the underground.

A slim operator bar sits at the edge of your terminal, showing active visitors, running doors, and signals from the network. It’s subtle, but alive — a heartbeat of your node.

The UNN Room Client doesn’t change SSH. It simply adds a hidden layer beneath it, giving every user the power to operate a node, host services, and shape their corner of the network. It’s the control panel for your room in the underground.

When you are connected you can idle in your room. The room server provides a interactive environment:
- **Room Chat**: Talk to visitors or bots.
- **Enhanced Interaction**: Supports command history (arrow keys) and smooth terminal handling.
- **Door Access**: Use commands starting with `/` to enter doors or run applications.
- **Interruption**: You can press **Ctrl+C** to interrupt a running door and return to the room prompt without being disconnected.

The client has two types of doors: 
1. Applications: These are doors that are run locally on the client. When a user enters your room they can start an application, the applications has an entry in the room chat (prefixed with slash) and shows the number of people that are currently using the application. The application can be started by typing /appname. The application starts fullscreen and can be interrupted by **Ctrl+C**. 
2. Agents: These are chat bots that have a presence in the chat of the room. They have a name and can react to anything. You can address them by typing @agentname followed by a message. The agent will then respond to the message. 

## Authentication

### User Identity
When connecting to the entry point, the client uses a **User Key** to authenticate.
- **Source**: It automatically detects your system key (`~/.ssh/id_ed25519` or `~/.ssh/id_rsa`).
- **Registration**: Before starting the client, you must register your public key manually with the entry point (via `/register`). Registration is **strictly enforced**—the client will exit with a fatal error if connection or registration fails.

### Visitor Identity (P2P Auth)
- **Visitor Verification**: The room server no longer allows anonymous connections. Every visitor must be pre-authorized by the entry point.
- **Handover**: The entry point signals the visitor's authenticated public key to your client, which then pre-authorizes that specific key for the P2P connection.
- **Strict Verification**: Any attempt to connect directly to the room using an unauthorized key will be rejected immediately.

### Room Identity
- **Host Key**: The client generates a temporary, **ephemeral** SSH host key for your room server.
- **Validity**: This key is valid only as long as the room session exists.
- **Verification**: Visitors verify this ephemeral key via the secure handshake coordinated by the entry point (automated by `unn-ssh` or via manual fingerprint check).