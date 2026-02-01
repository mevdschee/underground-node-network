# File Structure

This document provides an overview of the directory structure and the main components of the Underground Node Network (UNN).

## Directory Tree

```text
.
├── cmd/                # Executables
│   ├── unn-client/     # Client application
│   ├── unn-entrypoint/ # Entrypoint server
│   ├── unn-room/       # Room server (P2P host)
│   └── unn-intro/      # Interactive project intro (demo)
├── internal/           # Core library and logic (internal to UNN)
│   ├── entrypoint/     # Entrypoint server logic & hub
│   ├── sshserver/      # Room SSH server implementation
│   ├── ui/             # TUI components and terminal logic
│   │   ├── banner/     # Header display
│   │   ├── bridge/     # SSH <-> TUI event bridge
│   │   ├── common/     # Shared drawing/parsing utilities
│   │   ├── form/       # Multi-field forms
│   │   ├── input/      # Command line input
│   │   ├── log/        # Scrollable message log
│   │   ├── password/   # Masked input prompts
│   │   ├── popup/      # OSC-triggered popups
│   │   └── sidebar/    # Status and navigation sidebars
│   ├── protocol/       # Shared JSON messaging protocol
│   ├── nat/            # Hole-punching and STUN logic
│   └── doors/          # Door (mod) management
├── docs/               # Project documentation
│   ├── apps/           # Application-specific guides
│   ├── architecture/   # Technical design and structure
│   └── concepts/       # Detailed behavioral explanations
├── tests/              # End-to-end integration tests
└── doors/              # Example door binaries/scripts
```

## Main Component Descriptions

### Executables (`cmd/`)
- **unn-client**: The client application for interacting with the network (entrypoint + room).
- **unn-entrypoint**: The central hub that manages room registration and NAT signaling.
- **unn-room**: The host software that turns a local SSH server into a network node.
- **unn-intro**: A visual demonstration tool to introduce users to the UNN concepts.

### Internal Logic (`internal/`)
- **entrypoint/ & sshserver/**: Contain the core SSH handling and orchestration logic for the two primary node types.
- **ui/**: A modular TUI library built on `tcell`, tailored for the UNN's aesthetic and functional needs.
- **protocol/**: Defines the common JSON structures used for signaling between nodes.
- **nat/**: Implements the P2P fabric logic, including STUN-based candidate gathering and hole-punching.
- **doors/**: Handles the execution and communication with "Doors" (external programs piped into the TUI).

### Documentation (`docs/`)
- **apps/**: User-facing documentation for running and configuring the various tools.
- **architecture/**: Technical deep-dives into how the system is put together.
- **concepts/**: Explanations of underlying technologies like NAT traversal and SSH-based TUI.
