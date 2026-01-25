We are using a secret port 44322 for SSH and a subprotocol (named unn-room), as this provides a clean separation of concerns. The main SSH session becomes the user interface and the unn-room subsystem provides the room functionality. Doors are implemented as SSH subsystems, similar to how SFTP works.

## Connection Flow

1. **Registration**: Users register at the entry point with their SSH public key using the `/register` command.

2. **Node Online**: When a user starts the UNN client, it spins up an ephemeral SSH server (e.g., on 127.0.0.1:2222) and announces itself to the entry point.

3. **Traversal Coordination**: The client advertises candidates (public IP guess, LAN IP, reverse tunnel port). The entry point coordinates hole-punching between peers.

4. **Interactive Discovery**: Visitors can connect to the entry point manually or via the `unn-ssh` wrapper.
   - **Manual**: Connect via SSH to browse rooms and register keys.
   - **Wrapper**: Uses `ssh://` URLs to automate the handshake and teleportation.

5. **Direct P2P Connection**: Once NAT traversal succeeds, visitors connect directly to the node over SSH. The entry point is out of the picture from this point—it never proxies room traffic.

## Interactive Mode & Persistence

The entry point provides an interactive shell for visitors:
- **BBS Experience**: Supports command history, arrow keys, and basic terminal interaction.
- **Hole-Punching Bridge**: When a visitor selects a room, the entry point sends a `[CONNECTION_DATA]` block containing P2P candidates and host keys.
- **Auto-Return**: The `unn-ssh` wrapper is designed to return the user to the entry point shell after they finish their session in a room, allowing for persistent network navigation.

## Advanced Terminal Handling

To ensure a high-quality user experience, the system implements:
- **StdinProxy (Wrapper)**: A managed stdin reader that avoids competition between the Go wrapper and the standard SSH client. It can pause and resume itself during SSH handoffs.
- **SIGWINCH Support**: Both the wrapper and the entry point server handle window change signals to ensure the PTY is correctly resized.
- **Async Door Execution**: Room doors are executed in separate goroutines, allowing the main interaction loop to remain responsive and intercept **Ctrl+C** to kill the door subprocess if requested.


