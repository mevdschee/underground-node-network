Project Brief: The UNN Entry Point

The UNN Entry Point is the quiet backbone of the Underground Network. It doesn't host rooms, run doors, or execute code. Instead, it serves as a rendezvous and signaling hub: it helps peers discover each other and coordinates NAT traversal so they can establish direct P2P connections. Once a connection is negotiated, the entry point is out of the picture—all traffic flows directly between peers over SSH.

Entry points can be run by anyone. Entry points can connect to each other to form a larger network, then it does not matter which entry point you connect to, you will always end up in the same network. 

Entry points share lists of active rooms with each other. To visit a room, the entry point coordinates hole-punching or reverse tunnel negotiation between the visitor and the node operator. The actual SSH connection is always direct—entry points never proxy room traffic.

Entry points are responsible for the good behavior of their users that they register SSH public keys for.

When a user connects through the UNN Client, the server opens a second, hidden line — a control channel that lets the node operator announce their doors, receive execution requests, and handle approvals.

The server’s role is to maintain the illusion of a vast, distributed underground — a network made not of machines, but of people. Nodes appear and vanish as users come and go. Services flicker online, run their course, and disappear again. The server keeps the whole thing coherent without ever becoming the center of it.

It is the spine of the network, but never the brain. A silent switchboard in the dark. A map that redraws itself every time someone logs in.
