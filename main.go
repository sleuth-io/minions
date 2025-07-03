package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"coding-agent-dashboard/internal/api"
	"coding-agent-dashboard/internal/config"
	"coding-agent-dashboard/internal/git"
	"coding-agent-dashboard/internal/state"
)

var (
	hookMode   = flag.Bool("hook", false, "Run in hook mode (no web UI)")
	minionMode = flag.Bool("minion", false, "Run in minion mode (execute command transparently)")
	port       = flag.String("port", "8030", "Port to run the web server on")
)

func main() {
	flag.Parse()

	if *minionMode {
		// Minion mode - execute command transparently and exit
		if err := handleMinionMode(); err != nil {
			log.Printf("Minion mode error: %v", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

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

func handleMinionMode() error {
	// Create debug log file for minion mode
	debugFile, err := os.OpenFile("/tmp/minion-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer debugFile.Close()
		log.SetOutput(debugFile)
	} else {
		// Fallback to discard if debug file can't be created
		log.SetOutput(io.Discard)
	}
	
	// Get command arguments (everything after the --minion flag)
	args := flag.Args()
	if len(args) == 0 {
		return fmt.Errorf("minion mode requires at least one command argument")
	}

	// Get current working directory to identify this minion
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	// Initialize config directory and state manager for message watching
	configDir, err := config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	stateManager, err := state.NewManager(configDir)
	if err != nil {
		return fmt.Errorf("failed to initialize state manager: %w", err)
	}
	defer stateManager.Close()

	// Create command with the first argument as the command and rest as args
	cmd := exec.Command(args[0], args[1:]...)

	// Check if stdin is available (not a terminal or has data)
	stat, err := os.Stdin.Stat()
	stdinIsTerminal := err != nil || (stat.Mode()&os.ModeCharDevice) != 0
	
	var stdinPipe io.WriteCloser
	var ptyMaster *os.File
	
	if stdinIsTerminal {
		// For terminal mode, create a pty so we can inject messages
		ptyMaster, err = pty.Start(cmd)
		if err != nil {
			return fmt.Errorf("failed to create pty: %w", err)
		}
		defer ptyMaster.Close()
		
		// Set the pty size to match the current terminal
		if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
			pty.Setsize(ptyMaster, ws)
		}
		
		// Put the real terminal in raw mode to properly forward key sequences
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to set terminal to raw mode: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)
		
		// Forward pty output to real stdout/stderr
		go io.Copy(os.Stdout, ptyMaster)
		
		// Forward real stdin to pty (in background)
		go io.Copy(ptyMaster, os.Stdin)
		
		// Use ptyMaster as our message injection point
		stdinPipe = ptyMaster
	} else {
		// For piped mode, use our pipe for message injection
		stdinPipe, err = cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to create stdin pipe: %w", err)
		}
		
		// Connect stdout and stderr directly for transparency
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Start the command (only for non-pty mode, pty.Start already started it)
	if !stdinIsTerminal {
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start command: %w", err)
		}
	}
	
	// Don't manipulate stdin in terminal mode
	
	// Debug: log that process started
	if debugFile, err := os.OpenFile("/tmp/minion-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		debugFile.WriteString(fmt.Sprintf("[%s] Started process: %s (PID: %d)\n", time.Now().Format("15:04:05"), args[0], cmd.Process.Pid))
		debugFile.WriteString(fmt.Sprintf("[%s] Sent initial newline to Claude\n", time.Now().Format("15:04:05")))
		debugFile.Close()
	}

	// Create channels for coordination
	stdinDone := make(chan bool)
	processExit := make(chan error, 1)
	stopTicker := make(chan bool)
	var processRunning bool = true
	var processRunningMutex sync.RWMutex
	
	// Copy from os.Stdin to the command's stdin in a goroutine
	go func() {
		defer close(stdinDone)
		if !stdinIsTerminal {
			// For piped stdin, copy everything
			io.Copy(stdinPipe, os.Stdin)
		}
		// For terminal stdin, don't copy anything but keep pipe open for message injection
	}()

	// Watch for minion messages and forward them to stdin
	messageTicker := time.NewTicker(500 * time.Millisecond)
	go func() {
		defer messageTicker.Stop()
		
		// Create debug log file
		var debugLog *os.File
		debugLog, err := os.OpenFile("/tmp/minion-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			defer debugLog.Close()
			debugLog.WriteString(fmt.Sprintf("[%s] Started minion message ticker for working dir: %s\n", time.Now().Format("15:04:05"), workingDir))
			debugLog.WriteString(fmt.Sprintf("[%s] Config dir: %s\n", time.Now().Format("15:04:05"), configDir))
			debugLog.WriteString(fmt.Sprintf("[%s] Entering message polling loop\n", time.Now().Format("15:04:05")))
		}
		
		for {
			select {
			case <-messageTicker.C:
				// Check for pending messages
				message, err := stateManager.PopMinionMessage(workingDir)
				if err != nil {
					if debugLog != nil {
						debugLog.WriteString(fmt.Sprintf("[%s] Error checking minion messages: %v\n", time.Now().Format("15:04:05"), err))
					}
					continue
				}
				
				if message != nil {
					if debugLog != nil {
						debugLog.WriteString(fmt.Sprintf("[%s] Found message: %s\n", time.Now().Format("15:04:05"), message.Message))
					}
					
					// Check if process is still running before writing to stdin
					processRunningMutex.RLock()
					running := processRunning
					processRunningMutex.RUnlock()
					
					if !running {
						if debugLog != nil {
							debugLog.WriteString(fmt.Sprintf("[%s] Process not running, discarding message\n", time.Now().Format("15:04:05")))
						}
						continue
					}
					
					// Send message to child process stdin (only if we have a pipe)
					if stdinPipe != nil {
						// Send the message character by character, then Enter
						for _, char := range message.Message {
							stdinPipe.Write([]byte{byte(char)})
							time.Sleep(10 * time.Millisecond) // Small delay between characters
						}
						// Send Enter as carriage return
						_, err := stdinPipe.Write([]byte{'\r'})
						if err == nil {
							// Try to flush if it's a File
							if f, ok := stdinPipe.(*os.File); ok {
								f.Sync()
							}
						}
						if err != nil {
							if debugLog != nil {
								debugLog.WriteString(fmt.Sprintf("[%s] Error writing to stdin (pipe may be closed): %v\n", time.Now().Format("15:04:05"), err))
							}
							// Don't return here - the process might still be running, just stdin closed
							continue
						}
						if debugLog != nil {
							debugLog.WriteString(fmt.Sprintf("[%s] Successfully sent message '%s' + Enter to stdin\n", time.Now().Format("15:04:05"), message.Message))
						}
					} else {
						if debugLog != nil {
							debugLog.WriteString(fmt.Sprintf("[%s] No stdin pipe available for message injection\n", time.Now().Format("15:04:05")))
						}
					}
				}
			case <-processExit:
				if debugLog != nil {
					debugLog.WriteString(fmt.Sprintf("[%s] Message ticker exiting due to process exit\n", time.Now().Format("15:04:05")))
				}
				return
			case <-stopTicker:
				if debugLog != nil {
					debugLog.WriteString(fmt.Sprintf("[%s] Message ticker stopping\n", time.Now().Format("15:04:05")))
				}
				return
			}
		}
	}()

	// Wait for the command to complete in a goroutine
	go func() {
		processExit <- cmd.Wait()
	}()

	// Wait for process to exit (and stdin if it's not a terminal)
	if stdinIsTerminal {
		// For terminal stdin, just wait for process to exit
		err = <-processExit
		// Process exited, mark it as not running
		processRunningMutex.Lock()
		processRunning = false
		processRunningMutex.Unlock()
		
		// Stop the message ticker first
		close(stopTicker)
		// Give ticker time to stop
		time.Sleep(50 * time.Millisecond)
		// Then close stdin pipe
		stdinPipe.Close()
	} else {
		// For piped stdin, coordinate between process exit and stdin done
		select {
		case err = <-processExit:
			// Process exited, mark it as not running
			processRunningMutex.Lock()
			processRunning = false
			processRunningMutex.Unlock()
			
			// Stop the message ticker first
			close(stopTicker)
			// Give ticker time to stop
			time.Sleep(50 * time.Millisecond)
			// Then close stdin pipe
			stdinPipe.Close()
			// Wait a bit for any remaining stdin data
			select {
			case <-stdinDone:
			case <-time.After(100 * time.Millisecond):
			}
		case <-stdinDone:
			// Stdin closed, mark process as not running
			processRunningMutex.Lock()
			processRunning = false
			processRunningMutex.Unlock()
			
			// Stop ticker and close the pipe to signal EOF to subprocess
			close(stopTicker)
			time.Sleep(50 * time.Millisecond)
			stdinPipe.Close()
			// Now wait for process to complete
			err = <-processExit
		}
	}
	
	if err != nil {
		// Exit with the same exit code as the child process
		if exitError, ok := err.(*exec.ExitError); ok {
			os.Exit(exitError.ExitCode())
		}
		return fmt.Errorf("command execution failed: %w", err)
	}

	return nil
}
