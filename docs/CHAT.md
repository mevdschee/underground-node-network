Project Brief: The UNN Chat (The Underground Console)

The UNN Chat is the primary terminal for communication and navigation within the network. Heavily inspired by the golden era of **IRC** and **mIRC**, it translates early BBS aesthetics into a modern, security-conscious environment. 

## The Digital Channel

In the underground, every Room functions as a live channel. Communication is instantaneous, identity is strictly tied to your authenticated public key, and the atmosphere is built for speed and situational awareness.

### Interactive Features

*   **Real-time Sidebar**: Inspired by the classic mIRC user list, the sidebar provides an instant view of all travelers currently jacked into the node and the "Doors" (services) available for execution.
*   **Slash Command Navigation**: Direct control via intuitive commands like `/who`, `/files`, and `/clear`.
*   **Dynamic Mentions**: Interact with automated agents and bots using the `@name` syntax.
*   **Operator Focus**: Room owners possess an elevated console view, allowing them to monitor network signals and people status in real-time.

## The "mIRC" Palette

Information is prioritized via a curated color system, designed to allow operators to parse dense logs and high-speed chat at a single glance:

| Message Type | Aesthetic | Intent |
| :--- | :--- | :--- |
| **Self** | `Bold Green` | Your outgoing transmission. |
| **People** | `Yellow` | Inbound signals from other users. |
| **Server** | `Light Cyan` | Official room status and automated responses. |
| **Action** | `Light Blue` | Third-person narrative (e.g., `/me nods`). |
| **Whisper** | `Light Red` | Private encrypted messages. |
| **System** | `Dim Gray` | Technical background (joins, exits, banner data). |

- **Banner Art**: Support for high-impact ANSI art greets every traveler, preserving the classic BBS aesthetic of the room operator.

## Snappy Interactions

The UNN Chat is engineered for a "zero-lag" feel. Unlike traditional SSH sessions that can feel sluggish, the UNN environment uses specialized internal signaling to ensure:

- **Instant Redraws**: The screen remains alive and updates immediately when new data arrives, even if you aren't currently typing.
- **Seamsless Transitions**: Travelers can jump between the Chat UI and external Tools (Doors) with no delay. When a tool exits, the chat environment is restored instantly.

## Session Continuity & Privacy

The network prioritizes a balance between convenience and strict volatility:

- **Smart Reconnection**: If you disconnect and return (common during file transfers), the room remembers your previous messages and commands for that session.
- **Privacy-First Logging**: You only see what you were present for. History is personally isolated, and users cannot "back-read" conversations that occurred before they connected.
- **Strict Volatility**: All history is ephemeral. If the node is taken offline or restarted, the logs vanish.
- **Manual Purge**: The `/clear` command allows any user to instantly wipe their personal log and reset their screen.

## Core Commands

| Command | Effect |
| :--- | :--- |
| `/help` | List available room services. |
| `/who` | See who else is currently connected (name + hash). |
| `/doors` | List available sub-programs on this node. |
| `/files` | View the shared file manifest. |
| `/get <name>`| Initiate a secure, obfuscated file transfer. |
| `/clear` | Purge your display and server-side history. |
| `/open <door>` | Launch a specific application or game. |
| `/me <action>` | Perform an action. |
| `/whisper <user> <message>` | Send a private message to a user. |

If you have the same public key as the room client, your name will be prefixed with "@" (operator) and you will see these additional commands.

| Command | Effect |
| :--- | :--- |
| `/kick <user/hash> <reason>` | Kick a user from the room. |
| `/kickban <user/hash> <reason>` | Kick and ban a user from the room. |
| `/unban <user/hash>` | Unban a user from the room. |
| `/banlist` | List all banned people (name and hash). |
| `/lock <key>` | Lock the room with a key (required to enter). |
| `/unlock` | Unlock the room. |
| `/kickall <reason>` | Kick all other users from the room. |

