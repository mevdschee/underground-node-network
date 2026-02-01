# Out-Of-Band Signaling Reference

This document describes the parameters for messages sent between the Entrypoint and Room nodes over the `unn-control` SSH subsystem.

### Message Envelope
All messages use a standard JSON envelope:
```json
{"type": "...", "payload": {...}}
```

#### `register` / `unregister`
Sent by room nodes to manage their presence in the entrypoint's real-time directory.
- `room_name` (string): The globally unique handle for the room. Must be alphanumeric (4-20 chars).
- `doors` (string[]): The names of interactive "Door" applications available in this room.
- `candidates` (string[]): A list of connectivity candidates (Public IPs, LAN IPs, or Tunnel addresses) used by peers to attempt direct P2P connections.
- `ssh_port` (int): The TCP port where the room's local SSH server is listening (usually dynamic).
- `public_keys` (string[]): The room host's SSH public keys (in `authorized_keys` format). These are used by visitors to verify the host's identity during the P2P jump.
- `people_count` (int): The current occupancy of the room, used for discovery and load monitoring.

#### `room_list`
Sent by the entrypoint to a visitor, usually upon initial connection or a refresh request.
- `rooms` (object[]): An array of `RoomInfo` objects. Each object contains the same fields as the `register` payload, plus an `owner` (string) field indicating the username of the host.

#### `error`
A generic failure message used to communicate protocol violations or state errors.
- `message` (string): A human-readable description of the error (e.g., "Room name already taken").

#### `punch_request`
Initiated by a visitor when they select a room to join (e.g., via `/join`).
- `room_name` (string): The target room's handle.
- `candidates` (string[]): The visitor's own connectivity candidates, collected via STUN.
- `person_id` (string): A locally generated unique identifier for this specific connection session.

#### `punch_offer`
The entrypoint forwards the visitor's request to the room host.
- `person_id` (string): The session identifier from the visitor.
- `candidates` (string[]): The visitor's candidate list.
- `person_key` (string): The visitor's registered public key. The room host uses this to pre-authorize the incoming SSH connection.
- `username` / `display_name` (string): The visitor's identity metadata for logging and UI display.

#### `punch_answer`
The room host responds to the offer, providing their side of the P2P handshake.
- `person_id` (string): The session ID being answered.
- `candidates` (string[]): The room's final candidate list for this specific visitor.
- `ssh_port` (int): The specific port the room host wants the visitor to connect to.

#### `punch_start`
The entrypoint sends this to both parties simultaneously to trigger the actual TCP hole-punching.
- `room_name` (string): The destination room handle.
- `candidates` (string[]): The remote peer's candidates.
- `public_keys` (string[]): The remote peer's public keys for immediate authentication.
- `ssh_port` (int): The destination SSH port.
- `start_time` (int64): A Unix timestamp (milliseconds) used to synchronize the "dial" attempts on both sides for maximum NAT traversal success.
