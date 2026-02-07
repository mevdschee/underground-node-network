#!/bin/bash

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Building UDP Hole-Punch with QUIC Demo ===${NC}\n"

# Create bin directory
mkdir -p bin

# Build peer
echo -e "${YELLOW}Building peer...${NC}"
cd peer && go mod tidy && go build -o ../bin/peer && cd ..
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Peer built successfully${NC}"
else
    echo -e "${RED}✗ Peer build failed${NC}"
    exit 1
fi

# Build signaling server
echo -e "${YELLOW}Building signaling server...${NC}"
cd signaling-server && go mod tidy && go build -o ../bin/signaling-server && cd ..
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Signaling server built successfully${NC}"
else
    echo -e "${RED}✗ Signaling server build failed${NC}"
    exit 1
fi

echo -e "\n${GREEN}Build complete!${NC}"
echo -e "Binaries are in the ${BLUE}bin/${NC} directory\n"
