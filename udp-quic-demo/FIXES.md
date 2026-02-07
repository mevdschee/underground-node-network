# UDP-QUIC Demo - Error Fixes & Reorganization

## Problem 1: Type Redeclaration Errors
The `udp-quic-demo` had type redeclaration errors when running `go vet ./...`:
- `Candidate` type was declared in both `peer.go` and `signaling-server.go`
- `PeerInfo` type was declared in both `peer.go` and `signaling-server.go`

### Root Cause
Both `peer.go` and `signaling-server.go` were in the same `main` package and shared common type definitions. When Go tried to compile them together, it found duplicate type declarations.

### Solution
Created a new file `types.go` containing the shared type definitions and removed duplicates from both files.

## Problem 2: Project Organization
The original flat structure made it difficult to manage two separate applications in the same directory.

### Solution - Reorganized Project Structure
Reorganized the project into a cleaner structure with separate folders for each application:

```
udp-quic-demo/
├── bin/                      # Compiled binaries
│   ├── peer
│   └── signaling-server
├── peer/                     # Peer application
│   ├── main.go              # Peer implementation (was peer.go)
│   ├── types.go             # Shared types
│   └── go.mod               # Peer dependencies
├── signaling-server/         # Signaling server application
│   ├── main.go              # Server implementation (was signaling-server.go)
│   ├── types.go             # Shared types
│   └── go.mod               # Server dependencies
├── build.sh                  # Build script for both applications
├── setup.sh                  # Setup and installation script
├── README.md                 # Updated documentation
├── FIXES.md                  # This file
└── .gitignore               # Git ignore file
```

## Changes Made

### Files Created
1. **`peer/types.go`** - Shared type definitions for peer
2. **`signaling-server/types.go`** - Shared type definitions for server
3. **`peer/go.mod`** - Module definition for peer
4. **`signaling-server/go.mod`** - Module definition for server
5. **`build.sh`** - Build script to compile both applications
6. **`.gitignore`** - Git ignore file for build artifacts

### Files Moved/Renamed
1. **`peer.go`** → **`peer/main.go`**
2. **`signaling-server.go`** → **`signaling-server/main.go`**

### Files Deleted
1. **`types.go`** (root level - now in each app folder)
2. **`go.mod`** (root level - now in each app folder)
3. **`go.sum`** (root level - now in each app folder)

### Files Modified
1. **`README.md`** - Updated with new structure and instructions
2. **`setup.sh`** - Updated to work with new structure
3. **`FIXES.md`** - Updated with reorganization details

## Build Instructions

### Using the build script (recommended):
```bash
./build.sh
```

### Manual build:
```bash
cd peer && go mod tidy && go build -o ../bin/peer && cd ..
cd signaling-server && go mod tidy && go build -o ../bin/signaling-server && cd ..
```

### Run directly without building:
```bash
# Signaling server
cd signaling-server && go run .

# Peer (server mode)
cd peer && go run . -mode server -id server1 -port 9000

# Peer (client mode)
cd peer && go run . -mode client -id client1 -remote server1 -port 9001
```

## Benefits of New Structure

1. **Cleaner organization** - Each application has its own directory
2. **Independent modules** - Each app has its own `go.mod` for dependencies
3. **No redeclaration errors** - Each app is a separate package
4. **Easier to maintain** - Clear separation of concerns
5. **Standard Go layout** - Follows Go project conventions
6. **Build automation** - Simple build script for both apps

## Verification
All files now compile successfully:
- ✅ `./build.sh` builds both applications
- ✅ `./setup.sh` sets up and builds everything
- ✅ No type redeclaration errors
- ✅ Each application is independently buildable
- ✅ Binaries are organized in `bin/` directory
