# UNN OSC Protocol Documentation

The Underground Node Network uses OSC (Operating System Command) sequences for out-of-band communication between the entrypoint server, the client wrapper, and the node servers (rooms/doors).

## Format

All UNN-specific OSC messages use the code `9` and a JSON payload.

**Sequence:** `\x1b]9;<json_payload>\x07`

Where `<json_payload>` is a JSON object containing at least an `"action"` field.

---

## Server to Client (Entrypoint/Room -> Wrapper)

### `reconnect`
Sent by the entrypoint to trigger an automatic reconnect to a room after hole-punching is complete.

**Parameters:**
- `room_name` (string): Name of the room.
- `candidates` (array of strings): P2P candidates.
- `ssh_port` (int): SSH port of the room.
- `public_keys` (array of strings): Authorized public keys for the room.

**Payload Example:**
```json
{
  "action": "reconnect",
  "room_name": "lobby",
  "candidates": ["1.2.3.4", "5.6.7.8"],
  "ssh_port": 22,
  "public_keys": ["ssh-ed25519 ..."]
}
```

---

## Client to Server (Wrapper -> Entrypoint/Room)
*(Planned / Extensible)*

Currently received OSC messages are logged by the server. Future actions can be added here.

---

## Door to Server (Door -> Room Server)

Doors can emit OSC sequences to communicate with the room server. The room server intercepts these before they reach the client wrapper.

### `teleport`
*(Example candidate)*
Request the server to move the user to another room or trigger a specific network action.

**Parameters:**
- `target` (string): The target room or destination.

**Payload Example:**
```json
{
  "action": "teleport",
  "target": "other-room"
}
```
