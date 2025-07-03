package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type StatusChangeCallback func()

type FileWatcher struct {
	statusFile string
	manager    *Manager
	stopCh     chan bool
}

type Manager struct {
	configDir        string
	changeCallbacks  []StatusChangeCallback
	fileWatcher      *FileWatcher
}

func NewManager(configDir string) (*Manager, error) {
	manager := &Manager{
		configDir:       configDir,
		changeCallbacks: make([]StatusChangeCallback, 0),
	}
	
	// Set up file watcher for agent status file
	statusFile := filepath.Join(configDir, "agent-status.json")
	fileWatcher := &FileWatcher{
		statusFile: statusFile,
		manager:    manager,
		stopCh:     make(chan bool),
	}
	manager.fileWatcher = fileWatcher
	
	// Start file watching in a goroutine
	go fileWatcher.start()
	
	return manager, nil
}

func (m *Manager) GetRepositories() ([]Repository, error) {
	repoFile := filepath.Join(m.configDir, "repositories.json")
	
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		fmt.Printf("Repository file does not exist: %s\n", repoFile)
		return []Repository{}, nil
	}
	
	fmt.Printf("Loading repositories from: %s\n", repoFile)
	data, err := os.ReadFile(repoFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read repositories file: %w", err)
	}
	
	var repos []Repository
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse repositories file: %w", err)
	}
	
	fmt.Printf("Loaded %d repositories\n", len(repos))
	return repos, nil
}

func (m *Manager) SaveRepositories(repos []Repository) error {
	repoFile := filepath.Join(m.configDir, "repositories.json")
	
	data, err := json.MarshalIndent(repos, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal repositories: %w", err)
	}
	
	fmt.Printf("Saving repositories to: %s\n", repoFile)
	if err := os.WriteFile(repoFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write repositories file: %w", err)
	}
	
	fmt.Printf("Successfully saved %d repositories\n", len(repos))
	return nil
}

func (m *Manager) AddRepository(path, name string) (*Repository, error) {
	repos, err := m.GetRepositories()
	if err != nil {
		return nil, err
	}
	
	// Check if repository already exists
	for _, repo := range repos {
		if repo.Path == path {
			return nil, fmt.Errorf("repository already exists: %s", path)
		}
	}
	
	// Generate ID
	id := fmt.Sprintf("repo_%d", time.Now().Unix())
	
	newRepo := Repository{
		ID:        id,
		Path:      path,
		Name:      name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	
	repos = append(repos, newRepo)
	
	if err := m.SaveRepositories(repos); err != nil {
		return nil, err
	}
	
	return &newRepo, nil
}

func (m *Manager) RemoveRepository(id string) error {
	repos, err := m.GetRepositories()
	if err != nil {
		return err
	}
	
	// Filter out the repository
	var filteredRepos []Repository
	found := false
	for _, repo := range repos {
		if repo.ID != id {
			filteredRepos = append(filteredRepos, repo)
		} else {
			found = true
		}
	}
	
	if !found {
		return fmt.Errorf("repository not found: %s", id)
	}
	
	return m.SaveRepositories(filteredRepos)
}

func (m *Manager) GetAgentStatus() ([]AgentStatus, error) {
	statusFile := filepath.Join(m.configDir, "agent-status.json")
	
	if _, err := os.Stat(statusFile); os.IsNotExist(err) {
		return []AgentStatus{}, nil
	}
	
	data, err := os.ReadFile(statusFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent status file: %w", err)
	}
	
	var statuses []AgentStatus
	if err := json.Unmarshal(data, &statuses); err != nil {
		return nil, fmt.Errorf("failed to parse agent status file: %w", err)
	}
	
	return statuses, nil
}

func (m *Manager) AddStatusChangeCallback(callback StatusChangeCallback) {
	m.changeCallbacks = append(m.changeCallbacks, callback)
}

func (m *Manager) Close() {
	if m.fileWatcher != nil {
		m.fileWatcher.stop()
	}
}

func (m *Manager) notifyStatusChange() {
	for _, callback := range m.changeCallbacks {
		callback()
	}
}

func (m *Manager) SaveAgentStatus(statuses []AgentStatus) error {
	statusFile := filepath.Join(m.configDir, "agent-status.json")
	
	data, err := json.MarshalIndent(statuses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent status: %w", err)
	}
	
	if err := os.WriteFile(statusFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent status file: %w", err)
	}
	
	// Notify callbacks that status has changed
	m.notifyStatusChange()
	
	return nil
}

func (m *Manager) GetLastTranscriptMessage(transcriptPath string) (string, error) {
	if transcriptPath == "" {
		return "", nil
	}
	
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", nil // File might not exist yet
	}
	defer file.Close()
	
	var lastUserMessage string
	var lastAssistantMessage string
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}
		
		// Only process role-based messages (actual conversation content)
		if role, ok := entry["role"].(string); ok {
			if content, ok := entry["content"].(string); ok && content != "" {
				// Skip system messages and other non-conversation content
				if role == "system" {
					continue
				}
				
				// Clean up content by removing tool call blocks
				cleanContent := m.cleanMessageContent(content)
				if cleanContent != "" && !m.isSystemOutput(cleanContent) {
					if role == "assistant" {
						lastAssistantMessage = cleanContent
					} else if role == "user" {
						lastUserMessage = cleanContent
					}
				}
			}
		}
		// Skip non-role-based entries as they're likely system/hook output
	}
	
	// Prefer assistant messages (responses to user) over user messages
	result := lastAssistantMessage
	if result == "" {
		result = lastUserMessage
	}
	
	// Truncate if too long
	if len(result) > 200 {
		result = result[:200] + "..."
	}
	
	return result, nil
}

func (m *Manager) cleanMessageContent(content string) string {
	// Remove tool call blocks like <function_calls>...</function_calls>
	toolCallRegex := regexp.MustCompile(`(?s)<function_calls>.*?</function_calls>`)
	content = toolCallRegex.ReplaceAllString(content, "")
	
	// Remove empty lines and trim
	lines := strings.Split(content, "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanLines = append(cleanLines, trimmed)
		}
	}
	
	result := strings.Join(cleanLines, " ")
	return strings.TrimSpace(result)
}

func (m *Manager) isSystemOutput(content string) bool {
	// Check for patterns that indicate system/hook output rather than conversation
	systemPatterns := []string{
		"Config directory:",
		"Updated agent status:",
		"completed successfully:",
		"[1m", // ANSI color codes
		"[/home/", // Path patterns in system output
		"--hook",
		"Stop [",
		"___go_build_",
	}
	
	contentLower := strings.ToLower(content)
	for _, pattern := range systemPatterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true
		}
	}
	
	return false
}

// UpdateAgentLastMessage is deprecated - messages are now extracted directly in hook mode
func (m *Manager) UpdateAgentLastMessage(path, sessionID string) error {
	// This function is kept for backward compatibility but is no longer used
	// Messages are now extracted and stored directly in the hook handler
	return nil
}

func (m *Manager) GetLastTranscriptMessageFull(transcriptPath string) (string, error) {
	if transcriptPath == "" {
		return "", nil
	}
	
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", nil // File might not exist yet
	}
	defer file.Close()
	
	var lastUserMessage string
	var lastAssistantMessage string
	scanner := bufio.NewScanner(file)
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}
		
		// Only process role-based messages (actual conversation content)
		if role, ok := entry["role"].(string); ok {
			if content, ok := entry["content"].(string); ok && content != "" {
				// Skip system messages and other non-conversation content
				if role == "system" {
					continue
				}
				
				// Clean up content by removing tool call blocks but keep full length
				cleanContent := m.cleanMessageContent(content)
				if cleanContent != "" && !m.isSystemOutput(cleanContent) {
					if role == "assistant" {
						lastAssistantMessage = cleanContent
					} else if role == "user" {
						lastUserMessage = cleanContent
					}
				}
			}
		}
		// Skip non-role-based entries as they're likely system/hook output
	}
	
	// Prefer assistant messages (responses to user) over user messages
	result := lastAssistantMessage
	if result == "" {
		result = lastUserMessage
	}
	
	return result, nil
}

func (w *FileWatcher) start() {
	var lastModTime time.Time
	
	// Get initial modification time
	if stat, err := os.Stat(w.statusFile); err == nil {
		lastModTime = stat.ModTime()
	}
	
	ticker := time.NewTicker(1 * time.Second) // Check every second
	defer ticker.Stop()
	
	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			if stat, err := os.Stat(w.statusFile); err == nil {
				if stat.ModTime().After(lastModTime) {
					lastModTime = stat.ModTime()
					log.Printf("Agent status file changed, notifying callbacks")
					w.manager.notifyStatusChange()
				}
			}
		}
	}
}

func (w *FileWatcher) stop() {
	close(w.stopCh)
}

// AddMinionMessage adds a message for a minion in a specific directory
func (m *Manager) AddMinionMessage(path, message string) error {
	messages, err := m.GetMinionMessages(path)
	if err != nil {
		return err
	}

	newMessage := MinionMessage{
		ID:        fmt.Sprintf("msg_%d", time.Now().UnixNano()),
		Path:      path,
		Message:   message,
		Timestamp: time.Now(),
	}

	messages = append(messages, newMessage)
	return m.saveMinionMessages(path, messages)
}

// GetMinionMessages gets all pending messages for a minion in a specific directory
func (m *Manager) GetMinionMessages(path string) ([]MinionMessage, error) {
	messageFile := m.getMinionMessageFile(path)
	
	if _, err := os.Stat(messageFile); os.IsNotExist(err) {
		return []MinionMessage{}, nil
	}

	data, err := os.ReadFile(messageFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read minion messages file: %w", err)
	}

	var messages []MinionMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("failed to parse minion messages file: %w", err)
	}

	return messages, nil
}

// ClearMinionMessages removes all messages for a minion in a specific directory
func (m *Manager) ClearMinionMessages(path string) error {
	messageFile := m.getMinionMessageFile(path)
	return os.Remove(messageFile)
}

// PopMinionMessage gets the oldest message and removes it from the queue
func (m *Manager) PopMinionMessage(path string) (*MinionMessage, error) {
	messageFile := m.getMinionMessageFile(path)
	log.Printf("Checking for minion messages in file: %s (for path: %s)", messageFile, path)
	messages, err := m.GetMinionMessages(path)
	if err != nil {
		return nil, err
	}

	if len(messages) == 0 {
		return nil, nil
	}

	// Get the first message (oldest)
	message := messages[0]
	
	// Remove it from the slice
	remainingMessages := messages[1:]
	
	// Save the remaining messages
	if len(remainingMessages) == 0 {
		// If no messages left, remove the file
		return &message, m.ClearMinionMessages(path)
	} else {
		return &message, m.saveMinionMessages(path, remainingMessages)
	}
}

// saveMinionMessages saves messages to the file for a specific directory
func (m *Manager) saveMinionMessages(path string, messages []MinionMessage) error {
	messageFile := m.getMinionMessageFile(path)
	log.Printf("Saving minion messages to file: %s (for path: %s)", messageFile, path)
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(messageFile), 0755); err != nil {
		return fmt.Errorf("failed to create minion messages directory: %w", err)
	}

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal minion messages: %w", err)
	}

	if err := os.WriteFile(messageFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write minion messages file: %w", err)
	}

	return nil
}

// getMinionMessageFile returns the path to the message file for a specific directory
func (m *Manager) getMinionMessageFile(path string) string {
	// Create a safe filename from the path
	safeName := strings.ReplaceAll(path, "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	safeName = strings.ReplaceAll(safeName, ":", "_")
	if safeName == "" {
		safeName = "root"
	}
	return filepath.Join(m.configDir, "minion-messages", fmt.Sprintf("messages_%s.json", safeName))
}