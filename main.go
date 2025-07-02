package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
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

	// Determine event type from the presence of fields
	var event string
	if hookData.ToolInput != nil {
		event = "PreToolUse"
	} else if hookData.ToolOutput != nil {
		event = "PostToolUse"
	} else if hookData.ToolName != "" {
		event = "PostToolUse" // Fallback
	} else {
		event = "Stop" // Default for minimal data
	}

	// Update agent status based on hook event
	status := determineStatusFromEvent(event, hookData.ToolName)
	
	agentStatus := state.AgentStatus{
		Path:         workingDir,
		Status:       status,
		LastActivity: time.Now(),
		PID:          os.Getpid(),
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
		return "running"
	case "Stop":
		return "idle"
	default:
		return "unknown"
	}
}