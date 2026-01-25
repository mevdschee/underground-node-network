Project Brief: The UNN Entry Point

The UNN Entry Point is the quiet backbone of the Underground Network. It serves as a rendezvous and signaling hub: it helps peers discover each other and coordinates NAT traversal so they can establish direct P2P connections.

## Key Functions

### 1. Rendezvous & Discovery
Entry points maintain the directory of active rooms and announce them to visitors. They can also sync with other entry points to form a unified network.

### 2. Signaling Hub
The server coordinates hole-punching by relaying candidates and visitor identities. It never proxies room traffic; it only "introduces" peers so they can connect directly.

### 3. Identity Verification
Entry points enforce a **Manual Registration** policy. Once a user registers their public key (via `/register`), their username is claimed and protected.

## Visitor Handover

When a visitor connects to a room:
1. The entry point verifies the visitor's identity using their public key.
2. The verification status and the **marshaled public key** are signaled to the room operator.
3. This allows room operators to enforce strict public key authentication even in P2P mode.

## Manual Connection Support

For users not using the `unn-ssh` wrapper:
- The entry point provides simplified `ssh` command suggestions.
- It displays the **Expected Fingerprint** of the target room in standard OpenSSH format (e.g., `ED25519 key fingerprint is SHA256:...`).
- This allows manual users to securely verify room nodes with a single glance.

## Interactive Experience
The server provides a modern BBS experience inside SSH, with support for history, backspace, and real-time room updates.
