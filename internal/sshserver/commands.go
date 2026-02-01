package sshserver

import (
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/mevdschee/underground-node-network/internal/ui"
	"github.com/mevdschee/underground-node-network/internal/ui/bridge"
	"golang.org/x/crypto/ssh"
)

func (s *Server) handleCommand(channel ssh.Channel, sessionID string, input string) chan struct{} {
	s.mu.RLock()
	p := s.people[sessionID]
	s.mu.RUnlock()
	if p == nil {
		return nil
	}
	username := p.Username
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	if !strings.HasPrefix(input, "/") {
		// Regular chat message
		s.Broadcast(username, input)
		return nil
	}

	cmd := strings.TrimPrefix(input, "/")
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return nil
	}
	command := parts[0]

	if command == "get" || command == "download" {
		if len(parts) < 2 {
			fmt.Fprint(channel, "\rUsage: /get <filename>\r\n")
			return nil
		}
		fname := strings.TrimSpace(parts[1])
		s.mu.RLock()
		p := s.people[sessionID]
		s.mu.RUnlock()
		if p != nil {
			p.PendingDownload = fname
		}
		return nil
	}

	if command == "open" {
		if len(parts) < 2 {
			fmt.Fprint(channel, "\rUsage: /open <door>\r\n")
			return nil
		}
		doorName := parts[1]
		// Try to execute as a door
		if _, ok := s.doorManager.Get(doorName); ok {
			fmt.Fprintf(channel, "\r[Opening door: %s]\r\n", doorName)
			done := make(chan struct{})
			go func() {
				// Get current person to access bridge
				s.mu.RLock()
				p := s.people[sessionID]
				s.mu.RUnlock()

				var input io.Reader = channel
				if p != nil && p.Bridge != nil {
					input = p.Bus
				}

				output := bridge.NewOSCDetector(
					channel, func(action string, params map[string]interface{}) {
						s.HandleOSC(p, action, params)
					})
				output.UNNAware = p.UNNAware

				if err := s.doorManager.Execute(doorName, input, output, output); err != nil {
					fmt.Fprintf(channel, "\r[Door error: %v]\r\n", err)
				}
				fmt.Fprintf(channel, "\r[Closed door: %s]\r\n", doorName)
				close(done)
			}()
			return done
		} else {
			fmt.Fprintf(channel, "\rDoor not found: %s\r\n", doorName)
		}
		return nil
	}

	return nil
}

func (s *Server) handleInternalCommand(p *Person, cmd string) bool {
	if strings.HasPrefix(cmd, "/") {
		log.Printf("Internal command from %s: %s", p.Username, cmd)
		// Echo the command in the chat history
		parts := strings.SplitN(strings.TrimPrefix(cmd, "/"), " ", 2)
		command := parts[0]
		pubHash := s.getPubKeyHash(p.PubKey)

		addMessage := func(text string, msgType ui.MessageType) {
			p.ChatUI.AddMessage(text, msgType)
			s.mu.Lock()
			s.addMessageToHistory(pubHash, ui.Message{Text: text, Type: msgType})
			s.mu.Unlock()
		}

		// Always echo the command itself
		addMessage(cmd, ui.MsgCommand)

		switch command {
		case "help":
			addMessage("--- Available Commands ---", ui.MsgServer)
			addMessage("/help         - Show this help", ui.MsgServer)
			addMessage("/people       - List people in room", ui.MsgServer)
			addMessage("/doors        - List available doors", ui.MsgServer)
			addMessage("/files        - List available files", ui.MsgServer)
			addMessage("/get <file>   - Download a file", ui.MsgServer)
			addMessage("/clear        - Clear your chat history", ui.MsgServer)
			addMessage("/open <door>  - Open a door (launch program)", ui.MsgServer)
			addMessage("/quit [msg]   - Leave the room", ui.MsgServer)
			addMessage("Ctrl+C        - Exit room", ui.MsgServer)

			if s.isOperator(p.PubKey) {
				addMessage("--- Operator Commands ---", ui.MsgServer)
				addMessage("/kick <person> [reason]    - Kick a person", ui.MsgServer)
				addMessage("/kickban <person> [reason] - Kick and ban a person", ui.MsgServer)
				addMessage("/unban <person>            - Unban a person", ui.MsgServer)
				addMessage("/banlist                   - List banned people", ui.MsgServer)
				addMessage("/lock <key>                - Lock the room", ui.MsgServer)
				addMessage("/unlock                    - Unlock the room", ui.MsgServer)
				addMessage("/kickall [reason]          - Kick everyone", ui.MsgServer)
			}
			return true
		case "people":
			s.mu.RLock()
			people := make([]string, 0, len(s.people))
			for _, person := range s.people {
				prefix := ""
				if s.operatorPubKey != nil && string(person.PubKey.Marshal()) == string(s.operatorPubKey.Marshal()) {
					prefix = "@"
				}
				hash := s.getPubKeyHash(person.PubKey)
				if len(hash) > 8 {
					hash = hash[:8]
				}
				people = append(people, fmt.Sprintf("%s%s (%s)", prefix, person.Username, hash))
			}
			s.mu.RUnlock()
			addMessage("People:", ui.MsgServer)
			for _, personStr := range people {
				addMessage("• "+personStr, ui.MsgServer)
			}
			return true
		case "me":
			if len(parts) < 2 {
				addMessage("Usage: /me <action>", ui.MsgServer)
				return true
			}
			action := strings.TrimSpace(parts[1])
			chatMsg := fmt.Sprintf("* %s %s", p.Username, action)
			s.broadcastWithHistory(p.PubKey, chatMsg, ui.MsgAction)
			return true
		case "whisper":
			if len(parts) < 2 {
				addMessage("Usage: /whisper <user> <message>", ui.MsgServer)
				return true
			}
			msgParts := strings.SplitN(parts[1], " ", 2)
			if len(msgParts) < 2 {
				addMessage("Usage: /whisper <user> <message>", ui.MsgServer)
				return true
			}
			targetName := strings.TrimSpace(msgParts[0])
			whisperMsg := strings.TrimSpace(msgParts[1])

			s.mu.Lock()
			var target *Person
			for _, person := range s.people {
				if person.Username == targetName {
					target = person
					break
				}
			}
			s.mu.Unlock()

			if target == nil {
				addMessage(fmt.Sprintf("User '%s' not found.", targetName), ui.MsgServer)
				return true
			}

			// Add to sender
			addMessage(fmt.Sprintf("-> [%s] %s", targetName, whisperMsg), ui.MsgWhisper)
			// Add to target
			target.ChatUI.AddMessage(fmt.Sprintf("[%s] -> %s", p.Username, whisperMsg), ui.MsgWhisper)
			// Save to histories
			s.mu.Lock()
			senderHash := s.getPubKeyHash(p.PubKey)
			targetHash := s.getPubKeyHash(target.PubKey)
			s.addMessageToHistory(senderHash, ui.Message{Text: fmt.Sprintf("-> [%s] %s", targetName, whisperMsg), Type: ui.MsgWhisper})
			s.addMessageToHistory(targetHash, ui.Message{Text: fmt.Sprintf("[%s] -> %s", p.Username, whisperMsg), Type: ui.MsgWhisper})
			s.mu.Unlock()

			// Broadcast whisper event (the fact, not the content)
			bystanderMsg := fmt.Sprintf("* %s is secretly whispering with %s", p.Username, targetName)
			s.mu.Lock()
			for _, person := range s.people {
				if person.Username != p.Username && person.Username != targetName {
					person.ChatUI.AddMessage(bystanderMsg, ui.MsgSystem)
					h := s.getPubKeyHash(person.PubKey)
					s.addMessageToHistory(h, ui.Message{Text: bystanderMsg, Type: ui.MsgSystem})
				}
			}
			s.mu.Unlock()
			return true
		case "kick":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage("Usage: /kick <user/hash> [reason]", ui.MsgServer)
				return true
			}
			kickParts := strings.SplitN(parts[1], " ", 2)
			targetID := strings.TrimSpace(kickParts[0])
			reason := "No reason given."
			if len(kickParts) > 1 {
				reason = strings.TrimSpace(kickParts[1])
			}

			s.mu.Lock()
			var targetPerson *Person
			for _, person := range s.people {
				h := s.getPubKeyHash(person.PubKey)
				if person.Username == targetID || strings.HasPrefix(h, targetID) {
					targetPerson = person
					break
				}
			}
			s.mu.Unlock()

			if targetPerson == nil {
				addMessage("User not found.", ui.MsgServer)
				return true
			}

			s.Broadcast("Server", fmt.Sprintf("*** %s was kicked by @%s (%s) ***", targetPerson.Username, p.Username, reason))
			s.SendOSC(targetPerson, "popup", map[string]interface{}{
				"title":   "Kicked from Room",
				"message": fmt.Sprintf("You were kicked from %s.\nReason: %s", s.roomName, reason),
				"type":    "error",
			})
			time.Sleep(100 * time.Millisecond)
			targetPerson.Conn.Close()
			return true
		case "kickban":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage("Usage: /kickban <user/hash> [reason]", ui.MsgServer)
				return true
			}
			banParts := strings.SplitN(parts[1], " ", 2)
			targetID := strings.TrimSpace(banParts[0])
			reason := "No reason given."
			if len(banParts) > 1 {
				reason = strings.TrimSpace(banParts[1])
			}

			s.mu.Lock()
			var targetPerson *Person
			var targetHash string
			for _, person := range s.people {
				h := s.getPubKeyHash(person.PubKey)
				if person.Username == targetID || strings.HasPrefix(h, targetID) {
					targetPerson = person
					targetHash = h
					break
				}
			}
			if targetPerson != nil {
				s.bannedHashes[targetHash] = reason
				s.mu.Unlock()
				s.Broadcast("Server", fmt.Sprintf("*** %s was banned by @%s (%s) ***", targetPerson.Username, p.Username, reason))
				s.SendOSC(targetPerson, "popup", map[string]interface{}{
					"title":   "Banned from Room",
					"message": fmt.Sprintf("You were BANNED from %s.\nReason: %s", s.roomName, reason),
					"type":    "error",
				})
				time.Sleep(100 * time.Millisecond)
				targetPerson.Conn.Close()
			} else {
				// Handle offline ban by hash
				if len(targetID) >= 8 {
					s.bannedHashes[targetID] = reason
					s.mu.Unlock()
					addMessage(fmt.Sprintf("Banned hash prefix: %s", targetID), ui.MsgServer)
				} else {
					s.mu.Unlock()
					addMessage("User not found or hash too short.", ui.MsgServer)
				}
			}
			return true
		case "unban":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage("Usage: /unban <hash>", ui.MsgServer)
				return true
			}
			hash := strings.TrimSpace(parts[1])
			s.mu.Lock()
			found := false
			for h := range s.bannedHashes {
				if strings.HasPrefix(h, hash) {
					delete(s.bannedHashes, h)
					found = true
					break
				}
			}
			s.mu.Unlock()
			if found {
				addMessage(fmt.Sprintf("Unbanned hash prefix: %s", hash), ui.MsgServer)
			} else {
				addMessage("Ban not found.", ui.MsgServer)
			}
			return true
		case "banlist":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			addMessage("--- Banned Users ---", ui.MsgServer)
			s.mu.RLock()
			for h, r := range s.bannedHashes {
				addMessage(fmt.Sprintf("%s: %s", h[:12], r), ui.MsgServer)
			}
			s.mu.RUnlock()
			return true
		case "lock":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			if len(parts) < 2 {
				addMessage("Usage: /lock <key>", ui.MsgServer)
				return true
			}
			key := strings.TrimSpace(parts[1])
			s.mu.Lock()
			s.roomLockKey = key
			s.mu.Unlock()
			s.Broadcast("Server", fmt.Sprintf("*** @%s locked the room ***", p.Username))
			return true
		case "unlock":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			s.mu.Lock()
			s.roomLockKey = ""
			s.mu.Unlock()
			s.Broadcast("Server", fmt.Sprintf("*** @%s unlocked the room ***", p.Username))
			return true
		case "kickall":
			if !s.isOperator(p.PubKey) {
				addMessage("You do not have operator privileges.", ui.MsgServer)
				return true
			}
			reason := "All users kicked."
			if len(parts) > 1 {
				reason = strings.TrimSpace(parts[1])
			}
			s.Broadcast("Server", fmt.Sprintf("*** @%s is kicking everyone: %s ***", p.Username, reason))

			s.mu.Lock()
			for _, person := range s.people {
				if !s.isOperator(person.PubKey) {
					person.Conn.Close()
				}
			}
			s.mu.Unlock()
			return true
		case "get", "download":
			if len(parts) < 2 {
				addMessage("Usage: /get <filename>", ui.MsgServer)
				return true
			}
			fname := strings.TrimSpace(parts[1])
			p.PendingDownload = filepath.Clean(fname)
			p.ChatUI.Close(true)
			return true
		case "clear":
			s.mu.Lock()
			delete(s.histories, pubHash)
			s.mu.Unlock()
			p.ChatUI.ClearMessages()
			return true
		case "doors":
			doorList := s.doorManager.List()
			addMessage("--- Available doors ---", ui.MsgServer)
			for _, door := range doorList {
				addMessage("• "+door, ui.MsgServer)
			}
			addMessage("Type /open <door> to launch a program.", ui.MsgServer)
			return true
		case "files":
			s.showFiles(p.ChatUI)
			return true
		case "open":
			if len(parts) < 2 {
				addMessage("Usage: /open <door>", ui.MsgServer)
				return true
			}
			doorName := strings.TrimSpace(parts[1])
			if _, ok := s.doorManager.Get(doorName); !ok {
				addMessage(fmt.Sprintf("Door not found: %s", doorName), ui.MsgServer)
				return true
			}
			// Door exists, return false to exit TUI and execute it in handleCommand
			return false
		case "quit", "exit":
			if len(parts) > 1 {
				p.QuitReason = strings.TrimSpace(parts[1])
			}
			p.ChatUI.Close(true)
			return true
		default:
			// Check if this is a valid door
			if _, ok := s.doorManager.Get(command); ok {
				return false // Exit TUI to execute door
			}
			addMessage(fmt.Sprintf("Unknown command: %s", command), ui.MsgServer)
			return true
		}
	}
	return false
}
