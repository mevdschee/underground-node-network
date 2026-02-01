# In-Band (OSC 31337) Signaling Reference

This document describes the parameters for invisible ANSI OSC 31337 sequences embedded in the server's stdout stream.

### Message Format
```text
\x1b]31337;{"action": "...", ...}\x07
```

#### `teleport` (Action)
Instructs the `unn-client` to break the current entrypoint session and establish a direct P2P SSH connection.
- `action` (string): Fixed value `"teleport"`.
- `room_name` (string): The destination room handle.
- `candidates` (string[]): Remote peer's candidate IPs.
- `public_keys` (string[]): Remote peer's public keys for immediate authentication.
- `ssh_port` (int): The destination SSH port.
- `start_time` (int64): A Unix timestamp (milliseconds) for synchronized dialing.

#### `popup` (Action)
Triggers a modal-like UI notification box that suspends the main terminal interaction.
- `action` (string): Fixed value `"popup"`.
- `title` (string): Bold header text displayed at the top of the box.
- `message` (string): Body text (supports newlines).
- `type` (string): Visual theme indicator. Valid values: `"info"` (blue), `"warning"` (orange/yellow), `"error"` (red).

#### `transfer_block` (Action)
Transfers a single data chunk as part of a larger file download (Zmodem-like).
- `action` (string): Fixed value `"transfer_block"`.
- `filename` (string): The target local filename (path-sanitized by the client).
- `id` (string): A unique UUID for the transfer session, used to associate blocks with the correct `.parts` file.
- `index` (int): The sequence number of this block (0-indexed).
- `count` (int): The total number of blocks expected for this file.
- `checksum` (string): The SHA256 hex digest of the *complete* file. The client verifies this *after* reassembling all blocks.
- `data` (string): The binary payload, encoded using standard **Base64**. Chunks are typically 8KB (8192 bytes) before encoding.
