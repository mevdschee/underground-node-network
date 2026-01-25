### The Underground Node Network (UNN)

The Underground Node Network (UNN) is a distributed, SSH‑based digital underworld disguised as a retro‑styled BBS. It runs entirely over standard SSH, requiring no custom client, no special terminal, and no additional software to participate at the basic level. Anyone with ssh or PuTTY can jack in.

At its core, UNN is not a traditional BBS. Instead, it is a living mesh of user‑operated nodes, each one a personal “room” that comes online the moment a user connects. These rooms are not static message boards—they are computational spaces, capable of hosting interactive services, puzzles, bots, simulations, or tools. Each service behaves like a classic BBS “door,” but with a modern twist: services are executed locally by the user who hosts them, written in any programming language they choose.

The network is discovered through public entry points—server addresses that act as rendezvous and signaling hubs. Entry points do not have rooms of their own and never proxy traffic; they only help peers find each other and coordinate NAT traversal. Once a direct P2P connection is established via hole-punching or reverse tunnels, visitors connect directly to user nodes over SSH, and the entry point is out of the picture. Visitors can explore the topology, discover active nodes, and interact with the services those nodes expose.

Each user’s node is ephemeral, appearing only while they are connected. When active, it becomes a computing micro‑hub inside the underground network. Other visitors can enter that node, use its services, and interact with whatever the node owner has chosen to host—tools, games, experiments, data forges, or strange artifacts of code.

UNN is designed to feel like a clandestine hacker‑den ecosystem:
a shifting constellation of personal machines, each offering unique capabilities, all connected through a shared SSH‑based fabric. It is a programmable world, a social computing experiment, and a collaborative underground network—built entirely from text, terminals, and imagination.

## Connecting

You can connect to the network using the `unn-ssh.sh` wrapper script for a seamless experience:
```bash
./unn-ssh.sh ssh://localhost:44322/roomname
```
See [docs/SSH_WRAPPER.md](docs/SSH_WRAPPER.md) for more details.

