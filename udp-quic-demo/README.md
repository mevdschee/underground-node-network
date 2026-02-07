# UDP Hole-Punch with QUIC Demo

This is a standalone demonstration of UDP hole-punching with QUIC transport for NAT traversal.

## Architecture

```
┌─────────────┐         ┌──────────────────┐         ┌─────────────┐
│   Peer A    │         │ Signaling Server │         │   Peer B    │
│ (Behind NAT)│         │  (Public Server) │         │ (Behind NAT)│
└──────┬──────┘         └────────┬─────────┘         └──────┬──────┘
       │                         │                          │
       │  1. Register candidates │                          │
       ├────────────────────────►│                          │
       │                         │  2. Register candidates  │
       │                         │◄─────────────────────────┤
       │                         │                          │
       │  3. Get peer B info     │                          │
       ├────────────────────────►│                          │
       │                         │                          │
       │  4. UDP hole-punch packets                         │
       ├───────────────────────────────────────────────────►│
       │◄───────────────────────────────────────────────────┤
       │                         │                          │
       │  5. QUIC connection established                    │
       ├═══════════════════════════════════════════════════►│
       │                         │                          │
```

## Components

The project is organized into separate applications:

### 1. **Signaling Server** (`signaling-server/`)
   - HTTP server for peer discovery
   - Exchanges NAT candidates between peers
   - No data relay (only coordination)
   - Files:
     - `main.go` - Server implementation
     - `types.go` - Shared data structures
     - `go.mod` - Module dependencies

### 2. **Peer Client** (`peer/`)
   - STUN-based public IP discovery
   - UDP hole-punching
   - QUIC connection establishment
   - Can run as server or client
   - Files:
     - `main.go` - Peer implementation
     - `types.go` - Shared data structures
     - `go.mod` - Module dependencies

## How It Works

### 1. Candidate Discovery
Each peer discovers its connectivity candidates:
- **STUN**: Discovers public IP:port via Google STUN server
- **Local**: Enumerates local network interfaces

### 2. Signaling
Peers register their candidates with the signaling server via HTTP.

### 3. UDP Hole-Punching
The client peer:
- Retrieves the server peer's candidates
- Sends UDP packets to all candidates
- This "punches holes" in both NATs

### 4. QUIC Connection
After hole-punching:
- Client attempts QUIC connection to server's candidates
- QUIC provides TCP-like reliability over UDP
- Connection succeeds even through symmetric NAT (in many cases)

## Building

To build the executables:

```bash
cd udp-quic-demo

# Build both applications
cd peer && go mod tidy && go build -o ../bin/peer && cd ..
cd signaling-server && go mod tidy && go build -o ../bin/signaling-server && cd ..

# Or use the build script
./build.sh
```

The compiled binaries will be in the `bin/` directory.

## Usage

### Terminal 1: Start Signaling Server
```bash
cd udp-quic-demo
./bin/signaling-server

# Or run directly with go run
cd signaling-server && go run .
```

### Terminal 2: Start Server Peer
```bash
cd udp-quic-demo
./bin/peer -mode server -id server1 -port 9000

# Or run directly with go run
cd peer && go run . -mode server -id server1 -port 9000
```

### Terminal 3: Start Client Peer
```bash
cd udp-quic-demo
./bin/peer -mode client -id client1 -remote server1 -port 9001

# Or run directly with go run
cd peer && go run . -mode client -id client1 -remote server1 -port 9001
```

## Testing NAT Traversal

To test actual NAT traversal:

1. Run signaling server on a **public server** (VPS, cloud instance)
2. Run server peer on **Machine A** (behind NAT)
3. Run client peer on **Machine B** (behind different NAT)
4. Update `-signaling` flag to point to public server

Example:
```bash
# On Machine A
cd udp-quic-demo/peer
go run . -mode server -id serverA -port 9000 -signaling http://YOUR_PUBLIC_IP:8080

# On Machine B
cd udp-quic-demo/peer
go run . -mode client -id clientB -remote serverA -port 9000 -signaling http://YOUR_PUBLIC_IP:8080
```

## Expected Output

**Server Peer:**
```
Starting in server mode as server1 on port 9000
Discovering NAT candidates...
STUN discovered: 203.0.113.45:9000
Total candidates: 2
  - 203.0.113.45:9000
  - 192.168.1.100:9000
Registering with signaling server...
Registration successful
QUIC listener started on port 9000
Waiting for incoming connections...
Accepted connection from 198.51.100.67:9001
Received: Hello from UDP hole-punched QUIC connection!
```

**Client Peer:**
```
Starting in client mode as client1 on port 9001
Discovering NAT candidates...
STUN discovered: 198.51.100.67:9001
Total candidates: 2
  - 198.51.100.67:9001
  - 192.168.1.50:9001
Registering with signaling server...
Registration successful
Found remote peer with 2 candidates
  - 203.0.113.45:9000
  - 192.168.1.100:9000
Performing UDP hole-punch...
Sent punch packet to 203.0.113.45:9000
Sent punch packet to 192.168.1.100:9000
Attempting QUIC connection...
Successfully connected to 203.0.113.45:9000
QUIC connection established!
Sending: Hello from UDP hole-punched QUIC connection!
Received response: Echo: Hello from UDP hole-punched QUIC connection!
Demo completed successfully!
```

## Why This Works Better Than TCP

1. **UDP is stateless**: NATs are more permissive with UDP
2. **Simultaneous open**: Both sides send packets at the same time
3. **Port prediction**: Some NATs use predictable port allocation for UDP
4. **QUIC reliability**: Gets TCP-like guarantees over UDP

## Limitations

- **Symmetric NAT**: May still fail if both peers have strict symmetric NAT
- **Firewall rules**: Some firewalls block all unsolicited UDP
- **Port randomization**: Some NATs use cryptographic port randomization

## Next Steps

To integrate this into your UNN project:
1. Add UDP-based signaling to the entrypoint
2. Implement QUIC transport for SSH (or wrap SSH over QUIC)
3. Fall back to reverse tunnel if QUIC hole-punch fails
4. Use this as primary connection method before trying TCP

## Dependencies

- `github.com/quic-go/quic-go` - QUIC implementation in Go
