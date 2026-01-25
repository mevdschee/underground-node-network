Yes — the direct TCP stream can absolutely be an SSH connection, and in fact it’s one of the cleanest, most robust ways to build your P2P UNN links. SSH is just a protocol that runs over TCP, so once two peers have a negotiated TCP path (via NAT traversal or reverse tunnels), they can speak SSH over it just like they would to any server on the open internet.

Here’s the essence of it:

    The entry point helps two peers discover each other.

    The client (node operator) exposes a reachable TCP port through traversal.

    The visitor connects to that port.

    The visitor then performs a normal SSH handshake directly with the node.

From that moment on, the entry point is out of the picture.

This turns each node into a tiny SSH server that comes online only while the user is connected to the UNN.
Why SSH‑over‑P2P works beautifully
1. SSH already gives you authentication, encryption, and multiplexing

You don’t need to invent a secure protocol.
SSH gives you:

    encrypted channels

    multiple subchannels (terminal + control)

    identity verification

    port forwarding

    subsystem support

Perfect for your underground mesh.
2. It keeps the “normal terminal” experience intact

Visitors still see a terminal session when they enter a node.
They don’t need a special client.
They don’t need to know they’re in a P2P link.
3. It makes every node a first‑class SSH host

Each user’s machine becomes:

    a temporary SSH server

    reachable only through negotiated tunnels

    hosting its own doors

    exposing its own subsystems

It feels like a hidden machine in a secret network.
4. NAT traversal doesn’t break SSH

Once the TCP path exists, SSH works exactly as if the node were on a public IP.
How it fits into the UNN model
1. Entry point = rendezvous + signaling

It never proxies traffic.
It only helps peers find each other.
2. Client = ephemeral SSH server

The UNN client runs a lightweight SSH server locally, bound to a port like:
Code

127.0.0.1:2222

3. Traversal = exposing that port

The client advertises candidates:

    public IP guess

    LAN IP

    reverse tunnel port

The entry point coordinates hole‑punching.
4. Visitor = direct SSH connection

Once traversal succeeds, the visitor runs:
```bash
ssh -p <port> <candidate-ip>
```

 However, the **UNN SSH Wrapper (`unn-ssh`)** automates this process:
1. **Candidate Probing**: It quickly tests each candidate IP/port to find the one that works.
2. **Host Key Verification**: It extracts the node's ephemeral public keys from the signaling data and creates a temporary `known_hosts` file, ensuring a secure handshake without manual `StrictHostKeyChecking` prompts.
5. Doors = SSH subsystems

Just like SFTP is a subsystem, doors can be too.
The vibe

This design makes the network feel like a constellation of hidden SSH servers that appear only when their operators are online. Each node is a private machine behind layers of NAT, reachable only through negotiated tunnels and secret handshakes.

It’s elegant, minimal, and deeply on‑theme.