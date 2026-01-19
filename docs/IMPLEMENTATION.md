We are using a secret port 44322 for SSH and a subprotocol (named unn-room), as this provides a clean separation of concerns. The main SSH session becomes the user interface and the unn-room subsystem provides the room functionality. Doors are implemented as SSH subsystems, similar to how SFTP works.

## Connection Flow

1. **Registration**: Users register at the entry point with their SSH public key.

2. **Node Online**: When a user starts the UNN client, it spins up an ephemeral SSH server (e.g., on 127.0.0.1:2222) and announces itself to the entry point.

3. **Traversal Coordination**: The client advertises candidates (public IP guess, LAN IP, reverse tunnel port). The entry point coordinates hole-punching between peers.

4. **Direct P2P Connection**: Once NAT traversal succeeds, visitors connect directly to the node over SSH. The entry point is out of the picture from this point—it never proxies room traffic.

5. **Room Discovery**: The entry point provides a list of active rooms to visitors before they initiate direct connections.


