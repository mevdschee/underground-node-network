# UNN Entrypoint Architecture

The **UNN Entrypoint** acts as the central signaling hub and user directory. It is designed to handle many simultaneous terminal sessions while coordinating off-band P2P signaling.

### Component Overview

```mermaid
graph TD
    subgraph "SSH Layer"
        Listener[TCP Listener:44322]
        SSHD[SSH Server Engine]
    end

    subgraph "Session Management"
        Srv[Entrypoint Server]
        People[Person Registry]
        Rooms[Room Registry]
        DB[(Users Database)]
    end

    subgraph "Per-User Process"
        Bridge[Input Bridge]
        Bus[SSH Bus]
        TUI[Entry UI / Chat]
    end

    Listener --> SSHD
    SSHD --> Srv
    Srv --> People
    Srv --> Rooms
    Srv --> DB
    People --> Bridge
    Bridge --> Bus
    Bus --> TUI
```

### Key Modules

- **Server Engine (`internal/entrypoint`)**: Manages the lifecycle of both "Persons" (visitors) and "Operators" (room nodes). It handles the control subsystem for room registration.
- **Input Bridge**: Decouples the raw SSH network read from the TUI event loop. It allows for asynchronous signaling (like teleportation) without blocking the terminal input.
- **Entry UI (`internal/ui`)**: A `tcell`-based TUI that provides the "BBS" experience, including the registration form and the global chat lobby.
- **Registration Database**: A simple, file-backed atomic store that maps SSH public keys to UNN identities and external platform verification status.

### Data Flow: Role-based Handling
1. **Operators**: Connect via a specialized `unn-control` subsystem. They send periodic heartbeat-like registrations with their P2P candidates and active doors.
2. **Persons**: Enter the interactive TUI. They interact with the interface to browse rooms and initiate P2P "teleports."

---
See also: [Entrypoint Role](../apps/entrypoint.md) | [Signaling Pattern](../concepts/signaling.md)
