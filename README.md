### The Underground Node Network (UNN)

![UNN screenshot 1](screenshot1.png)

The Underground Node Network (UNN) is a distributed, SSH‑based digital underworld disguised as a retro‑styled BBS. It is accessible via **SSH over QUIC** (with a custom client) in any terminal, providing NAT-traversing P2P connections between nodes.

At its core, UNN is not a traditional BBS. Instead, it is a living mesh of user‑operated nodes, each one a personal “room” that comes online the moment a user connects. These rooms are not static message boards—they are computational spaces, capable of hosting interactive services, puzzles, bots, simulations, or tools. Each service behaves like a classic BBS “door,” but with a modern twist: services are executed locally by the user who hosts them, written in any programming language they choose.

![registration](registration.png)

The network is discovered through public entry points—server addresses that act as rendezvous and signaling hubs. Entry points do not have rooms of their own and never proxy traffic; they only help peers find each other and coordinate NAT traversal. Once a direct P2P connection is established via **UDP hole-punching and SSH over QUIC**, visitors connect directly to user nodes, and the entry point is out of the picture. Visitors can explore the topology, discover active nodes, and interact with the services those nodes expose.

Each user’s node is ephemeral, appearing only while they are connected. When active, it becomes a computing micro‑hub inside the underground network. Other visitors can enter that node, use its services, and interact with whatever the node owner has chosen to host—tools, games, experiments, data forges, or strange artifacts of code.

![UNN screenshot 2](screenshot2.png)

UNN is designed to feel like a clandestine hacker‑den ecosystem:
a shifting constellation of personal machines, each offering unique capabilities, all connected through a shared SSH‑based fabric. It is a programmable world, a social computing experiment, and a collaborative underground network—built entirely from text, terminals, and imagination.

![UNN intro](unn-intro.png)

## Connecting

The easiest way to explore the network is using the `unn-client.sh` tool with a URL:

```bash
./unn-client.sh unn://localhost
```

If you don't specify a room (as a path), you'll enter the entry point's interactive TUI, where you can:

- List active rooms with `/rooms`
- Join a room with `/join <roomname>`
- Exit with `/quit` or `/exit`

If you're not using the client, you can connect directly using any SSH client on port 44322 on the entry point. As a normal SSH client will not be able to understand the in-band **OSC 31337 commands**, so you will need to manually teleport to a room and also downloads are not supported.

### Hosting a Node
Becoming a part of the network is designed to be frictionless:

1. **Register Identity**: Connect to an entry point to link your SSH public key to a social account (GitHub/GitLab). This establishes your verified UNN username.
2. **Launch Room**: Run the UNN room node. It will automatically find your personal SSH key and register your room node using that identity:
   ```bash
   ./start-room.sh -room <yourname>
   ```
3. **P2P Ready**: Your node is now online. Visitors can `/join` your room, and you'll coordinate direct connections via the entrypoint hub.

## Documentation

### [Application Architectures](docs/apps/README.md)
- [Entrypoint](docs/apps/entrypoint.md) - Signaling hub and discovery back-bone.
- [Room Node](docs/apps/room.md) - Ephemeral SSH server for hosting rooms and doors.
- [UNN Client](docs/apps/client.md) - Automated teleportation and navigation tool.

### [Software Architecture](docs/architecture/README.md)
- [File Structure](docs/architecture/file-structure.md) - High-level directory tree and component map.
- [Entrypoint Internals](docs/architecture/entrypoint.md) - Component-level view of the hub.
- [Room Node Internals](docs/architecture/room.md) - Building blocks of the node server.
- [Client Internals](docs/architecture/client.md) - Under the hood of the navigation tool.
- [UI Components](docs/architecture/ui-components.md) - Modular TUI architecture, logs, and forms.

### [Network Concepts](docs/concepts/README.md)
- [Identity & Verification](docs/concepts/identity.md) - Decentralized trust and key registration.
- [P2P & NAT Traversal](docs/concepts/p2p-nat.md) - Hole-punching and direct TCP streams.
- [Signaling Protocol](docs/concepts/signaling.md) - Custom control subsystems and ANSI OSC 31337.
- [TUI & Interactions](docs/concepts/tui_and_doors.md) - BBS experience, Input Bridges, and Doors.
- [Technical Details](docs/concepts/implementation_details.md) - Deep dive into internal mechanics.

