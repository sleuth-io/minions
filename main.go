package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"coding-agent-dashboard/internal/api"
	"coding-agent-dashboard/internal/config"
	"coding-agent-dashboard/internal/git"
	"coding-agent-dashboard/internal/state"
)

var (
	hookMode = flag.Bool("hook", false, "Run in hook mode (no web UI)")
	port     = flag.String("port", "8030", "Port to run the web server on")
)

func main() {
	flag.Parse()

	// Initialize config directories
	configDir, err := config.GetConfigDir()
	if err != nil {
		log.Fatal("Failed to get config directory:", err)
	}
	fmt.Printf("Config directory: %s\n", configDir)

	// Initialize state manager
	stateManager, err := state.NewManager(configDir)
	if err != nil {
		log.Fatal("Failed to initialize state manager:", err)
	}

	// Initialize git manager
	gitManager := git.NewManager()

	if *hookMode {
		// Hook mode - execute webhook notification and exit
		if err := handleHookMode(stateManager); err != nil {
			log.Printf("Hook mode error: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Web mode - start the server
	server := api.NewServer(stateManager, gitManager)

	// Set up callback for state changes to broadcast via WebSocket
	stateManager.AddStatusChangeCallback(func() {
		server.BroadcastStatusUpdate()
	})

	fmt.Printf("Starting Coding Agent Dashboard on port %s\n", *port)

	if err := server.Start(*port); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

type HookData struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	ToolName       string `json:"tool_name,omitempty"`
	ToolInput      any    `json:"tool_input,omitempty"`
	ToolOutput     any    `json:"tool_output,omitempty"`
}

func handleHookMode(stateManager *state.Manager) error {
	// Read hook data from stdin
	var hookData HookData
	decoder := json.NewDecoder(os.Stdin)
	if err := decoder.Decode(&hookData); err != nil {
		return fmt.Errorf("failed to decode hook data: %w", err)
	}

	// Get working directory from current directory (where Claude is running)
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Debug: Log the hook data to a file to understand what we're receiving
	logFile := "/tmp/claude-hook-debug.log"
	if f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		debugMsg := fmt.Sprintf("[%s] Hook data - SessionID: %s, TranscriptPath: %s, ToolName: %s, ToolInput: %v, ToolOutput: %v\n", 
			time.Now().Format("2006-01-02 15:04:05"), hookData.SessionID, hookData.TranscriptPath, hookData.ToolName, hookData.ToolInput != nil, hookData.ToolOutput != nil)
		f.WriteString(debugMsg)
	}

	// Determine event type from the presence of fields and context
	var event string
	if hookData.ToolInput != nil {
		event = "PreToolUse"
	} else if hookData.ToolOutput != nil {
		event = "PostToolUse"
	} else if hookData.ToolName != "" {
		event = "PostToolUse" // Fallback
	} else {
		// For minimal data, check if session is active vs truly stopped
		// If we have an active session with transcript, it might be a notification
		if hookData.SessionID != "" && hookData.TranscriptPath != "" {
			// Check if the transcript shows we're waiting for user input
			if isWaitingForUser(hookData.TranscriptPath) {
				event = "Notification"
			} else {
				event = "Stop"
			}
		} else {
			event = "Stop" // Default for minimal data
		}
	}
	
	// Debug: Log the event determination
	if f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		defer f.Close()
		debugMsg := fmt.Sprintf("[%s] Determined event type: %s -> status: %s\n", 
			time.Now().Format("2006-01-02 15:04:05"), event, determineStatusFromEvent(event, hookData.ToolName))
		f.WriteString(debugMsg)
	}

	// Update agent status based on hook event
	status := determineStatusFromEvent(event, hookData.ToolName)
	
	// Special handling: ignore Stop events that come shortly after Notification events
	if event == "Stop" {
		if shouldIgnoreStop(stateManager, workingDir) {
			// Keep the current status instead of changing to idle
			if currentStatuses, err := stateManager.GetAgentStatus(); err == nil {
				for _, s := range currentStatuses {
					if s.Path == workingDir && (s.Status == "running" || s.Status == "waiting") {
						status = s.Status // Keep the current status (running or waiting)
						break
					}
				}
			}
		}
	}

	agentStatus := state.AgentStatus{
		Path:           workingDir,
		Status:         status,
		LastActivity:   time.Now(),
		PID:            os.Getpid(),
		SessionID:      hookData.SessionID,
		TranscriptPath: hookData.TranscriptPath,
	}

	// Load existing statuses
	statuses, err := stateManager.GetAgentStatus()
	if err != nil {
		return fmt.Errorf("failed to get agent status: %w", err)
	}

	// Update or add status for this path
	found := false
	for i, s := range statuses {
		if s.Path == workingDir {
			// Preserve existing session info if not provided in current hook
			if agentStatus.SessionID == "" {
				agentStatus.SessionID = s.SessionID
			}
			if agentStatus.TranscriptPath == "" {
				agentStatus.TranscriptPath = s.TranscriptPath
			}
			statuses[i] = agentStatus
			found = true
			break
		}
	}
	if !found {
		statuses = append(statuses, agentStatus)
	}

	// Save updated statuses
	if err := stateManager.SaveAgentStatus(statuses); err != nil {
		return fmt.Errorf("failed to save agent status: %w", err)
	}

	// Extract and store last message from transcript if available
	if hookData.SessionID != "" && hookData.TranscriptPath != "" {
		lastMessage, fullMessage := extractLastMessageFromTranscript(hookData.TranscriptPath)
		if lastMessage != "" {
			// Update the agent status with the extracted message
			for i, s := range statuses {
				if s.Path == workingDir {
					statuses[i].LastMessage = lastMessage
					statuses[i].FullLastMessage = fullMessage
					break
				}
			}
			
			// Save the updated statuses with the new message
			if err := stateManager.SaveAgentStatus(statuses); err != nil {
				fmt.Printf("Warning: failed to save updated agent status with message: %v\n", err)
			}
		}
	}

	fmt.Printf("Updated agent status: %s -> %s\n", workingDir, status)
	return nil
}

func determineStatusFromEvent(event, _ string) string {
	switch event {
	case "PreToolUse":
		return "running"
	case "PostToolUse":
		return "running"
	case "Notification":
		return "waiting"
	case "Stop":
		return "idle"
	default:
		return "unknown"
	}
}

func extractLastMessageFromTranscript(transcriptPath string) (string, string) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", ""
	}
	defer file.Close()
	
	var lastUserMessage, lastAssistantMessage string
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		
		// Check if this entry has a message field (Claude Code transcript format)
		if message, ok := entry["message"].(map[string]interface{}); ok {
			if role, ok := message["role"].(string); ok && (role == "user" || role == "assistant") {
				var content string
				
				if role == "user" {
					// User messages have content as string
					if userContent, ok := message["content"].(string); ok {
						content = userContent
					}
				} else if role == "assistant" {
					// Assistant messages have content as array of objects
					if contentArray, ok := message["content"].([]interface{}); ok {
						var textParts []string
						for _, item := range contentArray {
							if contentObj, ok := item.(map[string]interface{}); ok {
								if contentType, ok := contentObj["type"].(string); ok && contentType == "text" {
									if text, ok := contentObj["text"].(string); ok {
										textParts = append(textParts, text)
									}
								}
							}
						}
						content = strings.Join(textParts, " ")
					}
				}
				
				if content != "" {
					// Clean content by removing tool calls
					cleanContent := cleanMessageContent(content)
					if cleanContent != "" {
						if role == "assistant" {
							lastAssistantMessage = cleanContent
						} else if role == "user" {
							lastUserMessage = cleanContent
						}
					}
				}
			}
		}
	}
	
	// Prefer assistant messages over user messages
	fullMessage := lastAssistantMessage
	if fullMessage == "" {
		fullMessage = lastUserMessage
	}
	
	// Create truncated version for display
	truncatedMessage := fullMessage
	if len(truncatedMessage) > 200 {
		truncatedMessage = truncatedMessage[:200] + "..."
	}
	
	return truncatedMessage, fullMessage
}

func cleanMessageContent(content string) string {
	// Remove tool call blocks
	re := regexp.MustCompile(`(?s)<function_calls>.*?</function_calls>`)
	content = re.ReplaceAllString(content, "")
	
	// Clean up whitespace
	lines := strings.Split(content, "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanLines = append(cleanLines, trimmed)
		}
	}
	
	return strings.TrimSpace(strings.Join(cleanLines, " "))
}

func isWaitingForUser(transcriptPath string) bool {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return false
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	var lastEntry map[string]interface{}
	
	// Find the last entry in the transcript
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		lastEntry = entry
	}
	
	if lastEntry == nil {
		return false
	}
	
	// Check if the last entry indicates we're waiting for user input
	// This typically happens after an assistant message
	if entryType, ok := lastEntry["type"].(string); ok && entryType == "assistant" {
		return true
	}
	
	// Also check if there's a message with assistant role
	if message, ok := lastEntry["message"].(map[string]interface{}); ok {
		if role, ok := message["role"].(string); ok && role == "assistant" {
			return true
		}
	}
	
	return false
}

func shouldIgnoreStop(stateManager *state.Manager, workingDir string) bool {
	statuses, err := stateManager.GetAgentStatus()
	if err != nil {
		return false
	}
	
	for _, status := range statuses {
		if status.Path == workingDir {
			// If current status is "running" or "waiting" and was updated recently (within 10 seconds), ignore the stop
			timeSinceUpdate := time.Since(status.LastActivity)
			if (status.Status == "running" || status.Status == "waiting") && timeSinceUpdate < 10*time.Second {
				return true
			}
			break
		}
	}
	
	return false
}
