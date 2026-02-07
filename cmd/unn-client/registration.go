package main

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// handleRegistration handles the client-side registration flow
// This runs locally in the client and communicates with entrypoint via SSH subsystem
func handleRegistration(sshClient *ssh.Client, sshUsername string) (string, error) {
	// Create entrypoint client for API communication
	epClient, err := NewEntrypointClient(sshClient)
	if err != nil {
		return "", fmt.Errorf("failed to create entrypoint client: %w", err)
	}
	defer epClient.Close()

	// Check if already verified
	status, err := epClient.GetUserStatus("")
	if err != nil {
		return "", fmt.Errorf("failed to check user status: %w", err)
	}

	if status.Verified {
		fmt.Printf("✓ Already verified as: %s\n", status.Username)
		return status.Username, nil
	}

	// Not verified - show registration info
	fmt.Println("\n=== UNN Registration Required ===")
	fmt.Println("To join rooms, you need to verify your identity.")
	fmt.Println("We'll verify your SSH key against your platform account (GitHub, GitLab, etc.).\n")

	// Simple registration loop
	var platform, platformUser, unnUsername string

	for {
		// Prompt for platform
		fmt.Print("Platform (github/gitlab/sourcehut/codeberg) [github]: ")
		fmt.Scanln(&platform)
		if platform == "" {
			platform = "github"
		}
		platform = strings.ToLower(strings.TrimSpace(platform))

		// Prompt for platform username
		fmt.Print("Platform Username: ")
		fmt.Scanln(&platformUser)
		platformUser = strings.TrimSpace(platformUser)

		// Prompt for UNN username
		fmt.Printf("UNN Username [%s]: ", sshUsername)
		fmt.Scanln(&unnUsername)
		if unnUsername == "" {
			unnUsername = sshUsername
		}
		unnUsername = strings.TrimSpace(unnUsername)

		// Validate platform
		platforms := []string{"github", "gitlab", "sourcehut", "codeberg"}
		validPlatform := false
		for _, v := range platforms {
			if platform == v {
				validPlatform = true
				break
			}
		}
		if !validPlatform {
			fmt.Println("❌ Error: unsupported platform. Please use github, gitlab, source hut, or codeberg.\n")
			continue
		}

		if platformUser == "" {
			fmt.Println("❌ Error: platform username cannot be empty\n")
			continue
		}

		if len(unnUsername) < 3 {
			fmt.Println("❌ Error: UNN username too short (min 3 characters)\n")
			continue
		}

		if !isAlphanumeric(unnUsername) {
			fmt.Println("❌ Error: UNN username must be alphanumeric\n")
			continue
		}

		// Check if username is available
		statusCheck, err := epClient.GetUserStatus(unnUsername)
		if err != nil {
			fmt.Printf("❌ Error checking username: %v\n\n", err)
			continue
		}

		if statusCheck.IsTaken {
			fmt.Printf("❌ Error: username already taken by %s\n\n", statusCheck.TakenByPlatform)
			continue
		}

		// Attempt registration
		platformInfo := fmt.Sprintf("%s@%s", platformUser, platform)
		fmt.Printf("\nVerifying %s...\n", platformInfo)

		err = epClient.RegisterUser(unnUsername, platformInfo)
		if err != nil {
			if strings.Contains(err.Error(), "key not found") || strings.Contains(err.Error(), "404") {
				fmt.Printf("❌ Verification failed: Your SSH key was not found in your %s account\n", platform)
				fmt.Printf("   Make sure your key is added to https://%s.com/%s/settings/keys\n\n", platform, platformUser)
				continue
			}
			fmt.Printf("❌ Registration error: %v\n\n", err)
			continue
		}

		// Success!
		fmt.Printf("\n✓ Registration successful!\n")
		fmt.Printf("✓ You are now registered as: %s\n\n", unnUsername)
		return unnUsername, nil
	}
}

// is Alphanumeric checks if a string contains only alphanumeric characters, hyphens, and underscores
func isAlphanumeric(s string) bool {
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
