Project Brief: The UNN Entry Point

The UNN Entry Point is the quiet backbone of the Underground Network. It doesn’t host rooms, run doors, or execute code. Instead, it allows people to connect to the network and then lets them list and visit rooms. In case they use the Room client it also connects their room to the network.

Entry points can be run by anyone. Entry points can connect to each other to form a larger network, then it does not matter which entry point you connect to, you will always end up in the same network. 

Entry points connect to other entry points to retrieve lists of rooms, connecting to a room connected to another entry point requires the other entry point to provide proxy access to that room.

Entry point are reponsible for good behavior of their users that they register ssh public keys for. 

Entry points are also responsible for providing proxy access to the rooms that are connected to them and for their good behavior.

When a user connects through the UNN Client, the server opens a second, hidden line — a control channel that lets the node operator announce their doors, receive execution requests, and handle approvals.

The server’s role is to maintain the illusion of a vast, distributed underground — a network made not of machines, but of people. Nodes appear and vanish as users come and go. Services flicker online, run their course, and disappear again. The server keeps the whole thing coherent without ever becoming the center of it.

It is the spine of the network, but never the brain.
A silent switchboard in the dark.
A map that redraws itself every time someone logs in.