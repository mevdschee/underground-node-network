#!/bin/bash

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== UDP Hole-Punch with QUIC Demo ===${NC}\n"

# Check if we're in the right directory
if [ ! -d "peer" ] || [ ! -d "signaling-server" ]; then
    echo "Error: Run this script from the udp-quic-demo directory"
    exit 1
fi

# Install dependencies
echo -e "${YELLOW}Installing dependencies...${NC}"
cd peer && go mod tidy && cd ..
cd signaling-server && go mod tidy && cd ..

# Build applications
echo -e "\n${YELLOW}Building applications...${NC}"
./build.sh

echo -e "\n${GREEN}Setup complete!${NC}\n"

echo -e "${BLUE}To test the demo:${NC}"
echo -e "1. ${YELLOW}Terminal 1:${NC} ./bin/signaling-server"
echo -e "2. ${YELLOW}Terminal 2:${NC} ./bin/peer -mode server -id server1 -port 9000"
echo -e "3. ${YELLOW}Terminal 3:${NC} ./bin/peer -mode client -id client1 -remote server1 -port 9001"
echo ""
echo -e "${BLUE}Or run directly with go run:${NC}"
echo -e "1. ${YELLOW}Terminal 1:${NC} cd signaling-server && go run ."
echo -e "2. ${YELLOW}Terminal 2:${NC} cd peer && go run . -mode server -id server1 -port 9000"
echo -e "3. ${YELLOW}Terminal 3:${NC} cd peer && go run . -mode client -id client1 -remote server1 -port 9001"
echo ""
echo -e "${BLUE}For real NAT traversal testing:${NC}"
echo -e "Run the signaling server on a public server, then run peers on different networks"
echo ""
