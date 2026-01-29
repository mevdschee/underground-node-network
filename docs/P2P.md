Yes — the direct TCP stream can absolutely be an SSH connection, and in fact it’s one of the cleanest, most robust ways to build your P2P UNN links. SSH is just a protocol that runs over TCP, so once two peers have a negotiated TCP path (via NAT traversal or reverse tunnels), they can speak SSH over it just like they would to any server on the open internet.

Here’s the essence of it:
1. The entry point helps two peers discover each other.
2. The client (node operator) exposes a reachable TCP port through traversal.
3. The person connects to that port.
4. The person then performs a normal SSH handshake directly with the node.

From that moment on, the entry point is out of the picture.

This turns each node into a tiny SSH server that comes online only while the user is connected to the UNN.

## Why SSH‑over‑P2P works beautifully

### 1. SSH already gives you authentication, encryption, and multiplexing
You don’t need to invent a secure protocol. SSH gives you:
- encrypted channels
- multiple subchannels (terminal + control)
- identity verification
- port forwarding
- subsystem support

### 2. It keeps the “normal terminal” experience intact
persons still see a terminal session when they enter a node. They don’t need a special client. They don’t need to know they’re in a P2P link.

### 3. Identity Handover & Trust
The UNN uses a **trust-forwarding** model. Because you are already authenticated at the entry point, your public key is known.
- The entry point sends your verified key to the node operator.
- The operator's client pre-authorizes your key for the direct SSH link.
- This ensures that only users who have "jumped" through an entry point can enter a room.

### 4. Direct Verification
persons verify the room's ephemeral host key using data provided by the entry point. This provides a secure, zero-TOFU experience even when connecting to random user nodes.

## How it fits into the UNN model

### 1. Entry point = rendezvous + signaling
It never proxies traffic. It only helps peers find each other and verify identities.

### 2. Client = ephemeral SSH server
The UNN client runs a lightweight SSH server locally, bound to an ephemeral port.

### 3. Traversal = exposing that port
The client advertises candidates (public IP guess, LAN IP, reverse tunnels). The entry point coordinates hole‑punching.

### 4. person = direct SSH connection
Once traversal succeeds, the person connects.
The **UNN SSH Wrapper (`unn-ssh`)** automates this:
1. **Candidate Probing**: Tests each candidate IP/port.
2. **Auth Handover**: Uses the entry point session identity to authenticate with the node.
3. **Host Key Verification**: Automatically populates `known_hosts` to ensure a secure, warning-free connection.

## The Vibe
This design makes the network feel like a constellation of hidden SSH servers that appear only when their operators are online. Each node is a private machine behind layers of NAT, reachable only through negotiated tunnels and verified identities.