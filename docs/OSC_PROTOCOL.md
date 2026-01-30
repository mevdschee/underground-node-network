# UNN OSC Protocol Documentation

The Underground Node Network uses OSC (Operating System Command) sequences for out-of-band communication between the entrypoint server, the client wrapper, and the node servers (rooms/doors).

## Format

All UNN-specific OSC messages use the code `9` and a JSON payload.

**Sequence:** `\x1b]9;<json_payload>\x07`

Where `<json_payload>` is a JSON object containing at least an `"action"` field.

---

## Server to Client (Entrypoint/Room -> Wrapper)

### `teleport`
Sent by the entrypoint to trigger an automatic teleport to a room after hole-punching is complete. The client wrapper listens for this and initiates a p2p SSH session.

**Parameters:**
- `room_name` (string): Name of the room.
- `candidates` (array of strings): P2P candidates.
- `ssh_port` (int): SSH port of the room.
- `public_keys` (array of strings): Authorized public keys for the room.

**Payload Example:**
```json
{
  "action": "teleport",
  "room_name": "lobby",
  "candidates": ["1.2.3.4", "5.6.7.8"],
  "ssh_port": 22,
  "public_keys": ["ssh-ed25519 ..."]
}
```

---

### `popup`
Sent by the server to display a formatted message box in the client wrapper. Used for notifications like being kicked from a room.

**Parameters:**
- `title` (string): The title of the popup box.
- `message` (string): The multi-line message content.
- `type` (string, optional): One of `info`, `warning`, `error`. Affects the box color.

**Payload Example:**
```json
{
  "action": "popup",
  "title": "Kicked from Room",
  "message": "You were kicked from lobby.\nReason: Spamming",
  "type": "error"
}
```

