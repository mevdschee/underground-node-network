package main

import (
	"encoding/json"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// EntrypointClient wraps SSH subsystem communication with the entrypoint
type EntrypointClient struct {
	sshClient        *ssh.Client
	apiSession       *ssh.Session
	apiStdin         io.WriteCloser
	apiStdout        io.Reader
	signalingSession *ssh.Session
	signalingStdin   io.WriteCloser
	signalingStdout  io.Reader
}

// NewEntrypointClient creates a new entrypoint client using SSH subsystems
func NewEntrypointClient(sshClient *ssh.Client) (*EntrypointClient, error) {
	client := &EntrypointClient{
		sshClient: sshClient,
	}

	// Open unn-api subsystem session
	apiSession, err := sshClient.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create API session: %w", err)
	}

	apiStdin, err := apiSession.StdinPipe()
	if err != nil {
		apiSession.Close()
		return nil, fmt.Errorf("failed to get API stdin: %w", err)
	}

	apiStdout, err := apiSession.StdoutPipe()
	if err != nil {
		apiSession.Close()
		return nil, fmt.Errorf("failed to get API stdout: %w", err)
	}

	if err := apiSession.RequestSubsystem("unn-api"); err != nil {
		apiSession.Close()
		return nil, fmt.Errorf("failed to request unn-api subsystem: %w", err)
	}

	client.apiSession = apiSession
	client.apiStdin = apiStdin
	client.apiStdout = apiStdout

	return client, nil
}

// Close closes all subsystem sessions
func (c *EntrypointClient) Close() error {
	if c.apiSession != nil {
		c.apiSession.Close()
	}
	if c.signalingSession != nil {
		c.signalingSession.Close()
	}
	return nil
}

// API Message types (must match entrypoint/api_ssh.go)
const (
	APITypeRoomList     = "room_list"
	APITypeUserStatus   = "user_status"
	APITypeUserRegister = "user_register"
	APITypeResponse     = "response"
	APITypeError        = "error"
)

type apiMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type userStatusRequest struct {
	Username string `json:"username,omitempty"`
}

type userStatusResponse struct {
	Verified        bool   `json:"verified"`
	Username        string `json:"username,omitempty"`
	Platform        string `json:"platform,omitempty"`
	IsTaken         bool   `json:"is_taken"`
	TakenByPlatform string `json:"taken_by_platform,omitempty"`
}

type userRegisterRequest struct {
	UNNUsername  string `json:"unn_username"`
	PlatformInfo string `json:"platform_info"` // e.g. "user@github"
}

type userRegisterResponse struct {
	Status   string `json:"status"`
	Username string `json:"username"`
}

type roomInfo struct {
	Name        string   `json:"name"`
	Owner       string   `json:"owner"`
	PeopleCount int      `json:"people_count"`
	Doors       []string `json:"doors"`
	Candidates  []string `json:"candidates"`
	SSHPort     int      `json:"ssh_port"`
	PublicKeys  []string `json:"public_keys"`
}

// GetUserStatus checks if the current SSH key is verified and if a username is available
func (c *EntrypointClient) GetUserStatus(username string) (*userStatusResponse, error) {
	req := userStatusRequest{Username: username}
	payload, _ := json.Marshal(req)

	msg := apiMessage{
		Type:    APITypeUserStatus,
		Payload: payload,
	}

	// Send request
	encoder := json.NewEncoder(c.apiStdin)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send user_status request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(c.apiStdout)
	var response apiMessage
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Type == APITypeError {
		var errMsg map[string]string
		json.Unmarshal(response.Payload, &errMsg)
		return nil, fmt.Errorf("API error: %s", errMsg["message"])
	}

	var status userStatusResponse
	if err := json.Unmarshal(response.Payload, &status); err != nil {
		return nil, fmt.Errorf("failed to parse user status: %w", err)
	}

	return &status, nil
}

// RegisterUser registers a new user identity with the entrypoint
func (c *EntrypointClient) RegisterUser(unnUsername, platformInfo string) error {
	req := userRegisterRequest{
		UNNUsername:  unnUsername,
		PlatformInfo: platformInfo,
	}
	payload, _ := json.Marshal(req)

	msg := apiMessage{
		Type:    APITypeUserRegister,
		Payload: payload,
	}

	// Send request
	encoder := json.NewEncoder(c.apiStdin)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send user_register request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(c.apiStdout)
	var response apiMessage
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Type == APITypeError {
		var errMsg map[string]string
		json.Unmarshal(response.Payload, &errMsg)
		return fmt.Errorf("registration failed: %s", errMsg["message"])
	}

	var regResp userRegisterResponse
	if err := json.Unmarshal(response.Payload, &regResp); err != nil {
		return fmt.Errorf("failed to parse registration response: %w", err)
	}

	if regResp.Status != "registered" {
		return fmt.Errorf("unexpected registration status: %s", regResp.Status)
	}

	return nil
}

// GetRooms retrieves the list of active rooms from the entrypoint
func (c *EntrypointClient) GetRooms() ([]roomInfo, error) {
	msg := apiMessage{
		Type: APITypeRoomList,
	}

	// Send request
	encoder := json.NewEncoder(c.apiStdin)
	if err := encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send room_list request: %w", err)
	}

	// Read response
	decoder := json.NewDecoder(c.apiStdout)
	var response apiMessage
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if response.Type == APITypeError {
		var errMsg map[string]string
		json.Unmarshal(response.Payload, &errMsg)
		return nil, fmt.Errorf("API error: %s", errMsg["message"])
	}

	var rooms []roomInfo
	if err := json.Unmarshal(response.Payload, &rooms); err != nil {
		return nil, fmt.Errorf("failed to parse room list: %w", err)
	}

	return rooms, nil
}

// RequestPreparePunch requests the entrypoint to coordinate hole-punching with a room
func (c *EntrypointClient) RequestPreparePunch(roomName string, clientPeerID string, clientCandidates []string) error {
	req := struct {
		RoomName         string   `json:"room_name"`
		ClientPeerID     string   `json:"client_peer_id"`
		ClientCandidates []string `json:"client_candidates"`
	}{
		RoomName:         roomName,
		ClientPeerID:     clientPeerID,
		ClientCandidates: clientCandidates,
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal prepare_punch request: %w", err)
	}

	msg := apiMessage{
		Type:    "prepare_punch",
		Payload: payload,
	}

	encoder := json.NewEncoder(c.apiStdin)
	if err := encoder.Encode(msg); err != nil {
		return fmt.Errorf("failed to send prepare_punch: %w", err)
	}

	// Wait for response
	decoder := json.NewDecoder(c.apiStdout)
	var resp apiMessage
	if err := decoder.Decode(&resp); err != nil {
		return fmt.Errorf("failed to receive prepare_punch response: %w", err)
	}

	if resp.Type == "error" {
		var errMsg struct {
			Error string `json:"error"`
		}
		json.Unmarshal(resp.Payload, &errMsg)
		return fmt.Errorf("prepare_punch error: %s", errMsg.Error)
	}

	return nil
}
