Project Brief: The UNN Entry Point

The UNN Entry Point is the quiet backbone of the Underground Network. It doesn't host rooms, run doors, or execute code. Instead, it serves as a rendezvous and signaling hub: it helps peers discover each other and coordinates NAT traversal so they can establish direct P2P connections. Once a connection is negotiated, the entry point is out of the picture—all traffic flows directly between peers over SSH.

Entry points can be run by anyone. Entry points can connect to each other to form a larger network, then it does not matter which entry point you connect to, you will always end up in the same network. 

Entry points share lists of active rooms with each other. To visit a room, the entry point coordinates hole-punching or reverse tunnel negotiation between the visitor and the node operator. The actual SSH connection is always direct—entry points never proxy room traffic.

Entry points are responsible for the good behavior of their users that they register SSH public keys for.

When a user connects through the UNN Client, the server opens a second, hidden line — a control channel that lets the node operator announce their doors, receive execution requests, and handle approvals.

The server’s role is to maintain the illusion of a vast, distributed underground — a network made not of machines, but of people. Nodes appear and vanish as users come and go. Services flicker online, run their course, and disappear again. The server keeps the whole thing coherent without ever becoming the center of it.

It is the spine of the network, but never the brain. A silent switchboard in the dark. A map that redraws itself every time someone logs in.

## User Authentication

## User Authentication

The Entry Point enforces a **Manual Registration** policy for user identities.

1.  **Registration**: To claim a username, you must connect manually and use the `/register` command:
    ```bash
    ssh newuser@entrypoint
    # In the shell:
    /register ssh-ed25519 AAA...
    ```
2.  **Persistence**: The public key is stored on disk in the `users/` directory.
3.  **Enforcement**: Future connections as `newuser` MUST authenticate with the registered private key.
4.  **Sanitization**: Usernames must consist of alphanumeric characters, hyphens, and underscores.

This system ensures that usernames cannot be spoofed or stolen once claimed. Until registered, a username is available to anyone.

## Room Hosting

Users can register rooms (e.g., `myroom`). The room itself is ephemeral, but the user's identity is persistent.
- A user is identified as `username@entrypoint`.
- Room ownership is tied to the authenticated connection.
- A user can only manage rooms they have registered (or rooms are simply ephemeral and tied to the live connection).
