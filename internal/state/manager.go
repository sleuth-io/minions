package state

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"coding-agent-dashboard/internal/claude"
)

type StatusChangeCallback func()

type FileWatcher struct {
	statusFile string
	manager    *Manager
	stopCh     chan bool
}

type TranscriptWatcher struct {
	manager            *Manager
	agentStatusPath    string
	agentWatcher       *fsnotify.Watcher
	transcriptWatchers map[string]*fsnotify.Watcher // sessionID -> watcher
	projectToSession   map[string]string            // project path -> current sessionID
	knownRepos         map[string]bool              // path -> true (for filtering)
	mutex              sync.RWMutex
	stopCh             chan bool
}

type Manager struct {
	configDir         string
	changeCallbacks   []StatusChangeCallback
	fileWatcher       *FileWatcher
	systemActions     []SystemAction // In-memory storage for system actions
	actionsMutex      sync.RWMutex   // Mutex for thread-safe access to actions
	transcriptWatcher *TranscriptWatcher
	transcriptParser  *claude.TranscriptParser
	lastMessages      map[string]string // In-memory storage for last messages (path -> message)
	fullLastMessages  map[string]string // In-memory storage for full last messages (path -> message)
	messagesMutex     sync.RWMutex     // Mutex for thread-safe access to messages
}


func NewManager(configDir string, hookMode bool) (*Manager, error) {
	manager := &Manager{
		configDir:        configDir,
		changeCallbacks:  make([]StatusChangeCallback, 0),
		systemActions:    make([]SystemAction, 0),
		transcriptParser: claude.NewTranscriptParser(),
		lastMessages:     make(map[string]string),
		fullLastMessages: make(map[string]string),
	}
	
	// Set up file watcher for agent status file
	statusFile := filepath.Join(configDir, "agent-status.json")
	
	// Create the agent status file if it doesn't exist
	if err := manager.ensureAgentStatusFile(statusFile); err != nil {
		return nil, fmt.Errorf("failed to create agent status file: %w", err)
	}
	
	// Only set up watchers if not in hook mode
	if !hookMode {
		fileWatcher := &FileWatcher{
			statusFile: statusFile,
			manager:    manager,
			stopCh:     make(chan bool),
		}
		manager.fileWatcher = fileWatcher
		
		// Set up transcript watcher
		transcriptWatcher := NewTranscriptWatcher(manager, statusFile)
		manager.transcriptWatcher = transcriptWatcher
		
		// Start file watching in a goroutine
		go fileWatcher.start()
		
		// Start transcript watching
		if err := transcriptWatcher.Start(); err != nil {
			return nil, fmt.Errorf("failed to start transcript watcher: %w", err)
		}
		
		log.Printf("Started file and transcript watchers (non-hook mode)")
	} else {
		log.Printf("Skipping watchers initialization (hook mode)")
	}
	
	return manager, nil
}

// NewTranscriptWatcher creates a new TranscriptWatcher instance
func NewTranscriptWatcher(manager *Manager, agentStatusPath string) *TranscriptWatcher {
	return &TranscriptWatcher{
		manager:            manager,
		agentStatusPath:    agentStatusPath,
		transcriptWatchers: make(map[string]*fsnotify.Watcher),
		projectToSession:   make(map[string]string),
		knownRepos:         make(map[string]bool),
		stopCh:             make(chan bool),
	}
}

// Start begins the transcript watching system
func (tw *TranscriptWatcher) Start() error {
	// Create watcher for agent status file
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	tw.agentWatcher = watcher
	
	// Add agent status file to watch
	if err := tw.agentWatcher.Add(tw.agentStatusPath); err != nil {
		return fmt.Errorf("failed to watch agent status file: %w", err)
	}
	
	// Initialize known repositories
	tw.updateKnownRepos()
	
	// Start the main watching loop
	go tw.watchLoop()
	
	// Initial agent status check to set up any existing watchers
	tw.handleAgentStatusChange()
	
	log.Println("Started transcript watcher with fsnotify")
	return nil
}

// Stop stops the transcript watching system
func (tw *TranscriptWatcher) Stop() {
	close(tw.stopCh)
	
	// Close agent status watcher
	if tw.agentWatcher != nil {
		tw.agentWatcher.Close()
	}
	
	// Close all transcript watchers
	tw.mutex.Lock()
	for sessionID, watcher := range tw.transcriptWatchers {
		watcher.Close()
		delete(tw.transcriptWatchers, sessionID)
	}
	// Clear project to session mapping
	tw.projectToSession = make(map[string]string)
	tw.mutex.Unlock()
	
	log.Println("Stopped transcript watcher")
}

// watchLoop is the main event loop for the transcript watcher
func (tw *TranscriptWatcher) watchLoop() {
	for {
		select {
		case event, ok := <-tw.agentWatcher.Events:
			if !ok {
				return
			}
			
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("Agent status file changed: %s", event.Name)
				tw.handleAgentStatusChange()
			}
		case err, ok := <-tw.agentWatcher.Errors:
			if !ok {
				return
			}
			log.Printf("Agent status watcher error: %v", err)
		case <-tw.stopCh:
			return
		}
	}
}

// handleAgentStatusChange processes changes to the agent status file
func (tw *TranscriptWatcher) handleAgentStatusChange() {
	// Update known repositories
	tw.updateKnownRepos()
	
	// Read current agent status
	statuses, err := tw.manager.GetAgentStatus()
	if err != nil {
		log.Printf("Failed to read agent status: %v", err)
		return
	}
	
	log.Printf("Processing agent status change - found %d agent statuses", len(statuses))
	
	// Track which sessions should be watched
	activeSessionIDs := make(map[string]bool)
	
	// Process each agent status entry
	for _, status := range statuses {
		log.Printf("Checking agent: path=%s, sessionID=%s, transcriptPath=%s, status=%s", 
			status.Path, status.SessionID, status.TranscriptPath, status.Status)
		
		// Only monitor agents in known repositories
		if !tw.isKnownRepository(status.Path) {
			log.Printf("Skipping agent - path %s not in known repositories", status.Path)
			continue
		}
		
		var sessionID, transcriptPath string
		
		// If session/transcript info is missing, try to auto-discover
		if status.SessionID == "" || status.TranscriptPath == "" {
			log.Printf("Auto-discovering transcript for path: %s", status.Path)
			
			// Use the transcript parser to find the most recent transcript
			transcriptInfo, err := tw.manager.transcriptParser.FindMostRecentTranscript(status.Path)
			if err != nil {
				log.Printf("Failed to auto-discover transcript for %s: %v", status.Path, err)
				continue
			}
			
			if transcriptInfo == nil {
				log.Printf("No transcript found for path: %s", status.Path)
				continue
			}
			
			sessionID = transcriptInfo.SessionID
			transcriptPath = transcriptInfo.Path
			log.Printf("Auto-discovered transcript: session=%s, path=%s", sessionID, transcriptPath)
		} else {
			sessionID = status.SessionID
			transcriptPath = status.TranscriptPath
		}
		
		activeSessionIDs[sessionID] = true
		
		// Check if we need to stop watching an old session for this project
		tw.mutex.Lock()
		if oldSessionID, exists := tw.projectToSession[status.Path]; exists && oldSessionID != sessionID {
			log.Printf("Stopping old transcript watcher for project %s (old session: %s, new session: %s)", status.Path, oldSessionID, sessionID)
			if oldWatcher, exists := tw.transcriptWatchers[oldSessionID]; exists {
				oldWatcher.Close()
				delete(tw.transcriptWatchers, oldSessionID)
			}
		}
		
		// Update project to session mapping
		tw.projectToSession[status.Path] = sessionID
		tw.mutex.Unlock()
		
		// Add watcher if not already watching
		tw.mutex.RLock()
		_, exists := tw.transcriptWatchers[sessionID]
		tw.mutex.RUnlock()
		
		if !exists {
			log.Printf("Adding new transcript watcher for session %s", sessionID)
			tw.addTranscriptWatcher(sessionID, transcriptPath)
		} else {
			log.Printf("Already watching transcript for session %s", sessionID)
		}
	}
	
	// Remove watchers for sessions that are no longer active
	tw.mutex.Lock()
	for sessionID, watcher := range tw.transcriptWatchers {
		if !activeSessionIDs[sessionID] {
			log.Printf("Removing transcript watcher for inactive session: %s", sessionID)
			watcher.Close()
			delete(tw.transcriptWatchers, sessionID)
			
			// Also remove from project to session mapping
			for projectPath, mappedSessionID := range tw.projectToSession {
				if mappedSessionID == sessionID {
					delete(tw.projectToSession, projectPath)
					break
				}
			}
		}
	}
	tw.mutex.Unlock()
	
	// Correct status based on transcript analysis (startup detection)
	tw.correctStatusFromTranscripts()
	
	// Debug: Show current watched transcripts
	tw.DebugWatchedTranscripts()
}

// addTranscriptWatcher adds a watcher for a specific transcript file
func (tw *TranscriptWatcher) addTranscriptWatcher(sessionID, transcriptPath string) {
	// Check if transcript file exists
	if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
		log.Printf("Transcript file does not exist: %s", transcriptPath)
		return
	}
	
	// Create watcher for this transcript
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create watcher for transcript %s: %v", transcriptPath, err)
		return
	}
	
	// Add transcript file to watch
	if err := watcher.Add(transcriptPath); err != nil {
		log.Printf("Failed to watch transcript file %s: %v", transcriptPath, err)
		watcher.Close()
		return
	}
	
	tw.mutex.Lock()
	tw.transcriptWatchers[sessionID] = watcher
	tw.mutex.Unlock()
	
	log.Printf("Added transcript watcher for session %s: %s", sessionID, transcriptPath)
	
	// Start watching this transcript in a goroutine
	go tw.watchTranscript(sessionID, transcriptPath, watcher)
}

// watchTranscript watches a specific transcript file for changes
func (tw *TranscriptWatcher) watchTranscript(sessionID, transcriptPath string, watcher *fsnotify.Watcher) {
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("Transcript file changed: %s", event.Name)
				tw.handleTranscriptChange(transcriptPath)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Transcript watcher error for %s: %v", transcriptPath, err)
		case <-tw.stopCh:
			return
		}
	}
}

// handleTranscriptChange processes changes to a transcript file
func (tw *TranscriptWatcher) handleTranscriptChange(transcriptPath string) {
	// Find the agent status entry by extracting project path from transcript path
	statuses, err := tw.manager.GetAgentStatus()
	if err != nil {
		log.Printf("Failed to get agent status: %v", err)
		return
	}
	
	var targetStatus *AgentStatus
	var statusIndex int
	
	// Extract project path from transcript path
	// Example: ~/.claude/projects/-home-mrdon-dev-sleuth-minions/session.jsonl -> /home/mrdon/dev/sleuth-minions
	projectPath := tw.extractProjectPathFromTranscript(transcriptPath)
	if projectPath == "" {
		log.Printf("Could not extract project path from transcript: %s", transcriptPath)
		return
	}
	
	for i, status := range statuses {
		if status.Path == projectPath {
			targetStatus = &statuses[i]
			statusIndex = i
			break
		}
	}
	
	if targetStatus == nil {
		log.Printf("No agent status found for project path: %s (transcript: %s)", projectPath, transcriptPath)
		return
	}
	
	// Extract latest message using transcript parser
	lastMessage, err := tw.manager.transcriptParser.GetLastMessage(transcriptPath)
	if err != nil {
		log.Printf("Failed to get last message from transcript %s: %v", transcriptPath, err)
		return
	}
	
	fullMessage, err := tw.manager.transcriptParser.GetLastMessageFull(transcriptPath)
	if err != nil {
		log.Printf("Failed to get full last message from transcript %s: %v", transcriptPath, err)
		fullMessage = lastMessage // fallback
	}
	
	// Store messages in memory instead of JSON file
	tw.manager.messagesMutex.Lock()
	tw.manager.lastMessages[targetStatus.Path] = lastMessage
	tw.manager.fullLastMessages[targetStatus.Path] = fullMessage
	tw.manager.messagesMutex.Unlock()
	
	// Update agent status (without messages)
	statuses[statusIndex].LastActivity = time.Now()
	
	// Check if we should change status to "running"
	// If status is "waiting", check if the last message is newer than when status was set to waiting
	shouldChangeToRunning := false
	if lastMessage != "" && !tw.manager.transcriptParser.IsSystemOutput(lastMessage) {
		if targetStatus.Status == "waiting" {
			// Check the last message info (timestamp and role)
			lastMessageTime, lastMessageRole, err := tw.manager.transcriptParser.GetLastMessageTimestampAndRole(transcriptPath)
			if err != nil {
				log.Printf("Failed to get last message info: %v", err)
				shouldChangeToRunning = true // fallback to old behavior
			} else if lastMessageRole == "user" {
				// If the most recent message is from user, always change to running (new conversation starting)
				log.Printf("Status is waiting but most recent message is from user (%v), changing to running for path: %s", 
					lastMessageTime, targetStatus.Path)
				shouldChangeToRunning = true
			} else if lastMessageTime.Before(targetStatus.LastActivity) {
				// Assistant message that's older than last activity - don't change status
				log.Printf("Status is waiting and last message (%v, %s) is before last activity (%v), not changing status for path: %s", 
					lastMessageTime, lastMessageRole, targetStatus.LastActivity, targetStatus.Path)
				shouldChangeToRunning = false
			} else {
				// Assistant message that's newer than last activity - change to running
				log.Printf("Status is waiting but last message (%v, %s) is after last activity (%v), changing to running for path: %s", 
					lastMessageTime, lastMessageRole, targetStatus.LastActivity, targetStatus.Path)
				shouldChangeToRunning = true
			}
		} else if targetStatus.Status == "idle" || targetStatus.Status == "unknown" {
			shouldChangeToRunning = true
		} else {
			log.Printf("Status already %s for path: %s, not changing (message: %.50s...)", targetStatus.Status, targetStatus.Path, lastMessage)
		}
	} else {
		log.Printf("Message appears to be system output, not changing status %s for path: %s (message: %.50s...)", targetStatus.Status, targetStatus.Path, lastMessage)
	}
	
	if shouldChangeToRunning {
		oldStatus := targetStatus.Status
		newStatus := "running"
		log.Printf("STATUS CHANGE: %s -> %s for path: %s (reason: conversation message detected: %.50s...)", 
			oldStatus, newStatus, targetStatus.Path, lastMessage)
		statuses[statusIndex].Status = newStatus
	}
	
	// Save updated status (without race condition from messages)
	if err := tw.manager.SaveAgentStatus(statuses); err != nil {
		log.Printf("Failed to save updated agent status: %v", err)
	}
}

// extractProjectPathFromTranscript extracts the original project path from a transcript file path
// Example: ~/.claude/projects/-home-mrdon-dev-sleuth-minions/session.jsonl -> /home/mrdon/dev/sleuth-minions
func (tw *TranscriptWatcher) extractProjectPathFromTranscript(transcriptPath string) string {
	// Get the directory containing the transcript file
	dir := filepath.Dir(transcriptPath)
	
	// Extract the project directory name (last part of path)
	projectDirName := filepath.Base(dir)
	
	// Convert back from Claude's format: -home-mrdon-dev-sleuth-minions -> /home/mrdon/dev/sleuth-minions
	if !strings.HasPrefix(projectDirName, "-") {
		return ""
	}
	
	// Remove the leading dash
	projectPath := strings.TrimPrefix(projectDirName, "-")
	
	// We need to be smarter about converting dashes back to slashes
	// The original FindMostRecentTranscript function uses: strings.ReplaceAll(projectPath, "/", "-")
	// So we need to reverse this carefully
	// Split by dashes and reconstruct, but we need to handle the case where
	// directory names themselves contain dashes
	
	// For now, let's try a different approach - check against known agent statuses
	statuses, err := tw.manager.GetAgentStatus()
	if err != nil {
		return ""
	}
	
	// Try to find a matching agent status by checking if any path would generate this project dir name
	for _, status := range statuses {
		// Convert the status path to Claude's directory format
		// Example: /home/mrdon/dev/sleuth -> -home-mrdon-dev-sleuth
		expectedDirName := strings.ReplaceAll(status.Path, "/", "-")
		
		log.Printf("Comparing transcript dir '%s' with expected dir '%s' for path '%s'", projectPath, expectedDirName, status.Path)
		
		// The transcript directory name has the leading dash removed, so we need to add it back for comparison
		if expectedDirName == "-"+projectPath {
			return status.Path
		}
	}
	
	return ""
}

// updateKnownRepos updates the map of known repositories
func (tw *TranscriptWatcher) updateKnownRepos() {
	repos, err := tw.manager.GetRepositories()
	if err != nil {
		log.Printf("Failed to get repositories: %v", err)
		return
	}
	
	tw.mutex.Lock()
	tw.knownRepos = make(map[string]bool)
	for _, repo := range repos {
		tw.knownRepos[repo.Path] = true
	}
	tw.mutex.Unlock()
	
	log.Printf("Updated known repositories: %v", tw.getKnownReposList())
}

// getKnownReposList returns a list of known repository paths for logging
func (tw *TranscriptWatcher) getKnownReposList() []string {
	tw.mutex.RLock()
	defer tw.mutex.RUnlock()
	
	var paths []string
	for path := range tw.knownRepos {
		paths = append(paths, path)
	}
	return paths
}

// DebugWatchedTranscripts logs information about currently watched transcripts
func (tw *TranscriptWatcher) DebugWatchedTranscripts() {
	tw.mutex.RLock()
	defer tw.mutex.RUnlock()
	
	log.Printf("Currently watching %d transcript files:", len(tw.transcriptWatchers))
	
	// Get current agent statuses to find transcript paths and repo paths
	statuses, err := tw.manager.GetAgentStatus()
	if err != nil {
		log.Printf("Failed to get agent status for debug: %v", err)
		return
	}
	
	for sessionID := range tw.transcriptWatchers {
		// Find the corresponding agent status for this session
		for _, status := range statuses {
			if status.SessionID == sessionID {
				log.Printf("  - Session: %s, Repo: %s, Transcript: %s", 
					sessionID, status.Path, status.TranscriptPath)
				break
			}
		}
	}
}

// isKnownRepository checks if a path is a known repository
func (tw *TranscriptWatcher) isKnownRepository(path string) bool {
	tw.mutex.RLock()
	defer tw.mutex.RUnlock()
	
	// Check exact match
	if tw.knownRepos[path] {
		return true
	}
	
	// Check if path is a subdirectory of any known repo
	for repoPath := range tw.knownRepos {
		if strings.HasPrefix(path, repoPath+"/") {
			return true
		}
	}
	
	return false
}




func (m *Manager) GetRepositories() ([]Repository, error) {
	repoFile := filepath.Join(m.configDir, "repositories.json")
	
	if _, err := os.Stat(repoFile); os.IsNotExist(err) {
		fmt.Printf("Repository file does not exist: %s\n", repoFile)
		return []Repository{}, nil
	}
	
	data, err := os.ReadFile(repoFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read repositories file: %w", err)
	}
	
	var repos []Repository
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("failed to parse repositories file: %w", err)
	}
	
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
		log.Printf("Agent status JSON is corrupted, attempting recovery: %v", err)
		
		// Try to recover from common corruption patterns
		recoveredStatuses, recoveryErr := m.recoverCorruptedAgentStatus(data)
		if recoveryErr != nil {
			// If recovery fails, start fresh
			log.Printf("Recovery failed, starting with empty status: %v", recoveryErr)
			return []AgentStatus{}, nil
		}
		
		log.Printf("Successfully recovered %d agent statuses", len(recoveredStatuses))
		
		// Save the recovered data
		if saveErr := m.SaveAgentStatus(recoveredStatuses); saveErr != nil {
			log.Printf("Failed to save recovered agent status: %v", saveErr)
		}
		
		return recoveredStatuses, nil
	}
	
	return statuses, nil
}

// GetAgentStatusWithMessages returns agent statuses with last messages from memory
func (m *Manager) GetAgentStatusWithMessages() ([]AgentStatusWithMessages, error) {
	statuses, err := m.GetAgentStatus()
	if err != nil {
		return nil, err
	}
	
	m.messagesMutex.RLock()
	defer m.messagesMutex.RUnlock()
	
	var statusesWithMessages []AgentStatusWithMessages
	for _, status := range statuses {
		statusWithMessages := AgentStatusWithMessages{
			AgentStatus:     status,
			LastMessage:     m.lastMessages[status.Path],
			FullLastMessage: m.fullLastMessages[status.Path],
		}
		statusesWithMessages = append(statusesWithMessages, statusWithMessages)
	}
	
	return statusesWithMessages, nil
}

// correctStatusFromTranscripts analyzes transcripts to correct status on startup
func (tw *TranscriptWatcher) correctStatusFromTranscripts() {
	statuses, err := tw.manager.GetAgentStatus()
	if err != nil {
		log.Printf("Failed to get agent status for transcript analysis: %v", err)
		return
	}
	
	var hasStatusChanges bool
	
	for i, status := range statuses {
		// Only analyze agents in known repositories
		if !tw.isKnownRepository(status.Path) {
			continue
		}
		
		// Try to find the transcript for this agent
		transcriptInfo, err := tw.manager.transcriptParser.FindMostRecentTranscript(status.Path)
		if err != nil {
			log.Printf("Failed to find transcript for %s: %v", status.Path, err)
			continue
		}
		
		if transcriptInfo == nil {
			log.Printf("No transcript found for %s, keeping status as %s", status.Path, status.Status)
			continue
		}
		
		// Analyze the transcript to determine correct status
		detectedStatus := tw.manager.transcriptParser.DetermineSessionStatus(transcriptInfo.Path)
		
		if detectedStatus != status.Status {
			log.Printf("STARTUP STATUS CORRECTION: %s -> %s for path: %s (transcript analysis)", 
				status.Status, detectedStatus, status.Path)
			statuses[i].Status = detectedStatus
			hasStatusChanges = true
		} else {
			log.Printf("Status %s confirmed correct for path: %s (transcript analysis)", status.Status, status.Path)
		}
		
		// Set the last message from the transcript during startup
		if lastMessage, err := tw.manager.transcriptParser.GetLastMessage(transcriptInfo.Path); err == nil && lastMessage != "" {
			tw.manager.messagesMutex.Lock()
			tw.manager.lastMessages[status.Path] = lastMessage
			// Also get the full message
			if fullMessage, err := tw.manager.transcriptParser.GetLastMessageFull(transcriptInfo.Path); err == nil && fullMessage != "" {
				tw.manager.fullLastMessages[status.Path] = fullMessage
			}
			tw.manager.messagesMutex.Unlock()
		}
	}
	
	// Save updated statuses if any changes were made
	if hasStatusChanges {
		if err := tw.manager.SaveAgentStatus(statuses); err != nil {
			log.Printf("Failed to save corrected agent status: %v", err)
		} else {
			log.Printf("Saved corrected agent statuses based on transcript analysis")
		}
	}
}

func (m *Manager) AddStatusChangeCallback(callback StatusChangeCallback) {
	m.changeCallbacks = append(m.changeCallbacks, callback)
}

func (m *Manager) Close() {
	if m.fileWatcher != nil {
		m.fileWatcher.stop()
	}
	if m.transcriptWatcher != nil {
		m.transcriptWatcher.Stop()
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
	return m.transcriptParser.GetLastMessage(transcriptPath)
}



func (m *Manager) GetLastTranscriptMessageFull(transcriptPath string) (string, error) {
	if transcriptPath == "" {
		return "", nil
	}
	return m.transcriptParser.GetLastMessageFull(transcriptPath)
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

// System action tracking functions
func (m *Manager) AddAction(actionType string, description string) {
	m.AddActionWithCommand(actionType, description, "")
}

func (m *Manager) AddActionWithCommand(actionType string, description string, command string) {
	log.Printf("AddActionWithCommand called with type: %s, description: %s, command: %s", actionType, description, command)
	action := SystemAction{
		ID:          fmt.Sprintf("action_%d", time.Now().UnixNano()),
		Type:        actionType,
		Description: description,
		Command:     command,
		Timestamp:   time.Now(),
	}
	
	log.Printf("Acquiring actions mutex")
	m.actionsMutex.Lock()
	defer m.actionsMutex.Unlock()
	
	log.Printf("Adding action to slice")
	m.systemActions = append(m.systemActions, action)
	
	// Keep only the last 50 actions
	if len(m.systemActions) > 50 {
		m.systemActions = m.systemActions[len(m.systemActions)-50:]
	}
	
	log.Printf("Notifying status change")
	// Notify callbacks that actions have changed (non-blocking)
	go m.notifyStatusChange()
	log.Printf("AddAction completed")
}

func (m *Manager) GetSystemActions() ([]SystemAction, error) {
	m.actionsMutex.RLock()
	defer m.actionsMutex.RUnlock()
	
	// Return a copy of the slice to prevent race conditions
	actionsCopy := make([]SystemAction, len(m.systemActions))
	copy(actionsCopy, m.systemActions)
	
	return actionsCopy, nil
}

// recoverCorruptedAgentStatus attempts to recover from common JSON corruption patterns
func (m *Manager) recoverCorruptedAgentStatus(data []byte) ([]AgentStatus, error) {
	dataStr := string(data)
	
	// Common corruption: extra ']' at the end
	if strings.HasSuffix(dataStr, "]]") {
		log.Printf("Detected double-bracket corruption, attempting fix")
		fixedData := []byte(strings.TrimSuffix(dataStr, "]"))
		
		var statuses []AgentStatus
		if err := json.Unmarshal(fixedData, &statuses); err == nil {
			// Successfully fixed, but remove old fields if they exist
			return m.cleanOldFields(statuses), nil
		}
	}
	
	// Try to parse as old format and convert
	type OldAgentStatus struct {
		Path            string    `json:"path"`
		Status          string    `json:"status"`
		LastActivity    time.Time `json:"last_activity"`
		PID             int       `json:"pid,omitempty"`
		SessionID       string    `json:"session_id,omitempty"`
		TranscriptPath  string    `json:"transcript_path,omitempty"`
		LastMessage     string    `json:"last_message,omitempty"`
		FullLastMessage string    `json:"full_last_message,omitempty"`
	}
	
	var oldStatuses []OldAgentStatus
	if err := json.Unmarshal(data, &oldStatuses); err == nil {
		log.Printf("Successfully parsed as old format, converting to new format")
		var newStatuses []AgentStatus
		for _, old := range oldStatuses {
			newStatus := AgentStatus{
				Path:           old.Path,
				Status:         old.Status,
				LastActivity:   old.LastActivity,
				PID:            old.PID,
				SessionID:      old.SessionID,
				TranscriptPath: old.TranscriptPath,
			}
			newStatuses = append(newStatuses, newStatus)
		}
		return newStatuses, nil
	}
	
	return nil, fmt.Errorf("unable to recover corrupted JSON")
}

// cleanOldFields removes old message fields if they exist
func (m *Manager) cleanOldFields(statuses []AgentStatus) []AgentStatus {
	// The statuses are already in the correct format, just return them
	return statuses
}

// ensureAgentStatusFile creates the agent status file if it doesn't exist
func (m *Manager) ensureAgentStatusFile(statusFile string) error {
	// Check if file already exists
	if _, err := os.Stat(statusFile); err == nil {
		return nil // File already exists
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check status file: %w", err)
	}
	
	// Create the config directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(statusFile), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	
	// Create empty agent status file
	emptyStatuses := []AgentStatus{}
	data, err := json.MarshalIndent(emptyStatuses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal empty agent status: %w", err)
	}
	
	if err := os.WriteFile(statusFile, data, 0644); err != nil {
		return fmt.Errorf("failed to create agent status file: %w", err)
	}
	
	log.Printf("Created agent status file: %s", statusFile)
	return nil
}

