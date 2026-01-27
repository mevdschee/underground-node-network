# UNN Download Tool (unn-dl)

The `unn-dl` tool is a high-performance, visually stunning downloader designed for the Underground Node Network. It provides an immersive TUI (Terminal User Interface) that allows for concurrent background downloading and real-time destination editing.

## Features

### 1. Visually Stunning Background Animation
The tool features a unique progress visualization where the terminal background itself acts as a progress bar:
- **Spatial Mapping**: Every character on your terminal screen represents a specific part of the file being downloaded.
- **Dynamic Filling**: As the download progresses, the screen "fills" from the **bottom-left to the top-right**, row by row, representing the data moving into your local system.
- **Live Atmosphere**: The animation provides immediate, non-intrusive feedback on the download's state and distribution.

### 2. Concurrent TUI Overlay
The user interface is projected *over* the background animation using `tcell`:
- **Real-time Metrics**: Displays current download speed (MB/s), percentage complete, total file size, and estimated time of arrival (ETA).
- **Non-blocking Input**: You can edit the destination filename or path *while* the file is actively downloading in the background. The cursor remains active in the input box.
- **Two-Step Confirmation**: The tool only exits and returns to the room once **both** the download is 100% complete and the path has been confirmed, you have to press ENTER after the download succeeded to confirm the path.

### 3. High Performance & Integrity
- **Background Transfers**: Uses dedicated Go goroutines for SFTP transfers, ensuring the UI remains responsive even on slower connections.
- **Automatic Verification**: Automatically calculates and verifies the **SHA256 signature** of the downloaded file against the expected signature provided by the room server.
- **Secure Tunneling**: Connects via a local SSH tunnel provided by `unn-ssh`, maintaining the security of the P2P connection.

## Architecture

The tool operates using a decoupled architecture:
- **Download Routine**: Handles SSH connection, SFTP handshake, and streaming data to a temporary file.
- **UI Engine**: Refreshes the terminal state every 100ms, updating both the background animation and the overlay metrics.
- **Finalization Logic**: Moves the file from a temporary location to the confirmed destination only after successful verification.

## Integration

`unn-dl` is designed to be invoked automatically by the `unn-ssh` wrapper:
1. Inside a room, the user triggers a download via `/get <filename>`.
2. The room server prints a secure `[DOWNLOAD FILE]` metadata block.
3. `unn-ssh` captures this block, suspends the room session, and launches `unn-dl`.
4. After finalize, `unn-ssh` resumes the room session seamlessly.

## Manual Usage (Batch Mode)

For automated scripts, `unn-dl` supports a non-interactive batch mode:
```bash
./unn-dl -port 44323 -id <uuid> -file <name> -sig <sha256> -batch
```
This bypasses the TUI and saves the file directly to the default location.
