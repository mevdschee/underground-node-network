# UNN UI Component Architecture

The UNN uses a custom-built Terminal User Interface (TUI) layer on top of `tcell`. The interface is split into several reusable components that provide the "BBS" feel while maintaining high performance over SSH.

### 1. Banner (ANSI Header)
- **Controlled by**: `internal/ui.EntryUI`
- **Behavior**: Displays multi-line ANSI art at the top of the screen. It is drawn once but can be updated via `SetBanner`.
- **Usage**: Used in the Entrypoint to greet visitors and set the mood for each room.

### 2. Sidebars (Information Panels)
- **Controlled by**: `internal/ui.EntryUI` and `internal/ui.ChatUI`
- **Behavior**: Fixed-width vertical panels on the right side of the screen. They automatically truncate content to fit the remaining horizontal space.
- **Components**:
    - **Room List**: Shows active rooms, their owners, and short hashes of their public keys. (Entrypoint)
    - **People List**: Shows travelers currently jacked into the room. (Room Node)
    - **Doors List**: Displays interactive programs available for execution. (Room Node)

### 3. Message Log (Scrollable Feed)
- **Controlled by**: `internal/ui.EntryUI` and `internal/ui.ChatUI`
- **Behavior**: A line-wrapped, scrollable list of messages. It supports multiple message types (Server, System, Chat, Self) with distinct color coding.
- **History**: Implements in-memory persistence. In the Room Node, history is key-isolated (only showing messages from your current session duration).

### 4. Command Input (Prompt Bar)
- **Controlled by**: `internal/ui.EntryUI` and `internal/ui.ChatUI`
- **Behavior**: A single-line input field at the bottom of the screen. It supports standard editing keys (Backspace, Arrows) and command history (Up/Down).
- **Execution**: Intercepts `/` commands for local processing (e.g., `/help`, `/clear`) and treats other input as broadcast messages.

### 5. Form Layer (Modals)
- **Controlled by**: `internal/ui.EntryUI` (`PromptForm`)
- **Behavior**: Provides a structured, field-based input overlay. It supports tab-navigation between fields, real-time validation (e.g., alphanumeric only), and error message display.
- **Usage**: Primary tool for the Entrypoint registration process.

### 6. OSC Popups (Out-of-Band Overlay)
- **Controlled by**: Client-side logic in `unn-client`, triggered by server signals.
- **Behavior**: A centered, high-contrast box that clears the entire screen. It features 256-color borders and drop shadows.
- **Usage**: Used for critical notifications that require user attention (e.g., "Kicked from Room", "Duplicate Session").

### 7. Input Bridge (The Invisible UI)
- **Controlled by**: `internal/ui.InputBridge`
- **Behavior**: Not a visual component, but manages the handoff of input between the TUI and external sub-processes (Doors). It ensures that `Ctrl+C` behavior is consistent and that terminal resizes are propagated correctly to both the TUI and any active sub-program.

---
See also: [Entrypoint Internals](entrypoint.md) | [Room Node Internals](room.md) | [Client Internals](client.md)
