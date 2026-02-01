# UNN UI Component Architecture

The UNN uses a modular Terminal User Interface (TUI) layer built on top of `tcell`. The interface is split into specialized sub-packages to ensure reusability, testability, and a consistent "BBS" aesthetic across all applications.

## Package Structure (`internal/ui/`)

### 1. Banner (`banner/`)
- **Behavior**: Renders multi-line ANSI art and system headers.
- **Responsibility**: Provides the "first impression" of a room or entrypoint. Supports dynamic updates to the room name and operator info.

### 2. Sidebars (`sidebar/`)
- **Behavior**: Fixed-width vertical panels (usually 20-30 characters) on the right side of the screen.
- **Auto-layout**: Sidebars automatically calculate their own heights and handle content truncation.
- **Variants**:
    - **PeopleSidebar**: Integrated with the room's occupant list.
    - **RoomSidebar**: Displays active rooms in the global entrypoint.
    - **DoorSidebar**: Lists interactive programs available to the user.

### 3. Message Log (`log/`)
- **Behavior**: A thread-safe, scrollable feed of timestamped entries.
- **Features**:
    - **Automated Wrapping**: Uses `uniseg` for correct grapheme-aware text wrapping.
    - **Message Types**: Defined in `internal/ui/types.go` (System, Server, Chat, Self), each with curated color schemes.
    - **Efficiency**: Only re-wraps lines when the parent container's width changes.

### 4. Command Input (`input/`)
- **Behavior**: A single-line interactive prompt with a cursor.
- **Features**:
    - **History**: Local command history navigable with Up/Down arrows.
    - **Focus**: Manages its own internal state for the insertion point.

### 5. Form & Password (`form/`, `password/`)
- **Form Overlay**: Provides a structured, tab-navigable interface for multi-field input (e.g., registration). Now supports custom titles and consistent border styling.
- **Password Prompt**: A specialized centered modal that masks input with asterisks. Used for room locking and sensitive credential entry.

### 6. Bridge & SSHBus (`bridge/`)
- **InputBridge**: Manages the high-concurrency pipe between the SSH channel and the UI. It intercepts **OSC (Operating System Command)** sequences for out-of-band signals like file downloads or popups.
- **SSHBus**: A dedicated implementation of `tcell.Tty`. It allows the TUI components to interact with an SSH stream as if it were a local Tty, handling window resizing and EOF gracefully.

### 7. Popups (`popup/`)
- **Behavior**: Stylized overlays triggered by OSC signals (`\x1b]31337;...`).
- **Styling**: Distinctive shadows and high-contrast colors (e.g., Dark Red for warnings).

### 8. Common Utilities (`common/`)
- **Standardized Drawing**: Unified functions like `DrawBorder`, `FillRegion`, and `DrawText` ensuring pixel-perfect alignment across all components.
- **Terminal Parsing**: Helpers for decoding SSH pty-req and window-change payloads.

## Shared Data Types (`types.go`)
To prevent circular dependencies between components (e.g., a Sidebar needing to know about Message types), core structures like `Message`, `MessageType`, and `RoomInfo` are centralized in `internal/ui/types.go`.

---
See also: [File Structure](file-structure.md) | [Entrypoint Internals](entrypoint.md) | [Room Node Internals](room.md)
