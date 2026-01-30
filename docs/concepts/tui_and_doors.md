# TUI & Room Interaction

The user experience in UNN is centered around a terminal-based Chat UI and a pluggable application system called "Doors."

### The Chat Console
Every room node provides a live, IRC-inspired chat console.
- **Sidebars**: Show active users and available doors.
- **Multiplexing**: The console manages the transition between chat mode and door execution.
- **History**: Implements an in-memory, key-isolated history system that replays only the messages you were present for.

### Doors (Local Services)
Doors are external programs that travelers can run on the room server.
- **Language Agnostic**: Doors can be written in Shell, Python, Go, C, or any other language available on the host machine.
- **Fullscreen**: When a door is launched, it takes over the terminal completely.
- **Interactive**: Doors can be games (like 2048), information tools, or custom BBS services.
- **Controlled Exit**: The room server monitors the door process and gracefully restores the chat UI when the door exits.

### The Stdin Bridge
To prevent input loss and correctly handle terminal interrupts (like `Ctrl+C`), the UNN uses a custom **Input Bridge**.
- It manages the handoff of raw terminal bytes from the SSH channel to the active process.
- It intercepts server-level commands even when a TUI is active.
- It handles terminal resizing (`SIGWINCH`) transparently for the visitor.

---
See also: [Chat Aesthetics](../../docs/CHAT.md)
