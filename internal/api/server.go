package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"coding-agent-dashboard/internal/git"
	"coding-agent-dashboard/internal/state"
)

type SSEHub struct {
	connections map[chan string]bool
	mutex       sync.RWMutex
}

type Server struct {
	stateManager *state.Manager
	gitManager   *git.Manager
	hub          *SSEHub
}

type AddRepositoryRequest struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

type MinionMessageRequest struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewSSEHub() *SSEHub {
	return &SSEHub{
		connections: make(map[chan string]bool),
	}
}

func (h *SSEHub) AddConnection(ch chan string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.connections[ch] = true
}

func (h *SSEHub) RemoveConnection(ch chan string) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	delete(h.connections, ch)
	close(ch)
}

func (h *SSEHub) Broadcast(message interface{}) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()

	data, err := json.Marshal(message)
	if err != nil {
		log.Printf("Failed to marshal SSE message: %v", err)
		return
	}

	messageStr := fmt.Sprintf("data: %s\n\n", string(data))

	for ch := range h.connections {
		select {
		case ch <- messageStr:
		default:
			// Channel is blocked, remove it
			delete(h.connections, ch)
			close(ch)
		}
	}
}

func NewServer(stateManager *state.Manager, gitManager *git.Manager) *Server {
	return &Server{
		stateManager: stateManager,
		gitManager:   gitManager,
		hub:          NewSSEHub(),
	}
}

func (s *Server) Start(port string) error {
	// Serve static files from web-dist
	fs := http.FileServer(http.Dir("./web-dist"))
	http.Handle("/", fs)

	// API routes
	http.HandleFunc("/api/repositories", s.handleRepositories)
	http.HandleFunc("/api/repositories/", s.handleRepositoryByID)
	http.HandleFunc("/api/status", s.handleStatus)
	http.HandleFunc("/api/webhook/claude", s.handleClaudeWebhook)
	http.HandleFunc("/api/actions/open-ide", s.handleOpenIDE)
	http.HandleFunc("/api/suggestions/directories", s.handleDirectorySuggestions)
	http.HandleFunc("/api/hooks/status", s.handleHookStatus)
	http.HandleFunc("/api/hooks/install", s.handleHookInstall)
	http.HandleFunc("/api/minion/message", s.handleMinionMessage)
	http.HandleFunc("/api/system-commands", s.handleSystemCommands)
	http.HandleFunc("/events", s.handleSSE)

	fmt.Printf("Serving at http://localhost:%s\n", port)
	return http.ListenAndServe(":"+port, nil)
}

func (s *Server) handleRepositories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "GET":
		s.getRepositories(w, r)
	case "POST":
		s.addRepository(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRepositoryByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/api/repositories/")
	if path == "" {
		http.Error(w, "Repository ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "DELETE":
		s.removeRepository(w, r, path)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getRepositories(w http.ResponseWriter, r *http.Request) {
	repos, err := s.stateManager.GetRepositories()
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get repositories: %v", err), http.StatusInternalServerError)
		return
	}

	// Get worktrees and status for each repository
	var reposWithData []state.RepositoryWithWorktrees
	agentStatuses, _ := s.stateManager.GetAgentStatus()

	for _, repo := range repos {
		worktrees, err := s.gitManager.GetWorktrees(repo.Path)
		if err != nil {
			// If we can't get worktrees, still include the repo but with empty worktrees
			worktrees = []state.Worktree{}
		}

		// Find relevant status entries
		var repoStatuses []state.AgentStatus
		for _, status := range agentStatuses {
			// Check if status path matches this repo or any of its worktrees
			for _, wt := range worktrees {
				if status.Path == wt.Path {
					repoStatuses = append(repoStatuses, status)
					break
				}
			}
		}

		repoWithData := state.RepositoryWithWorktrees{
			Repository: repo,
			Worktrees:  worktrees,
			Status:     repoStatuses,
		}
		reposWithData = append(reposWithData, repoWithData)
	}

	json.NewEncoder(w).Encode(reposWithData)
}

func (s *Server) addRepository(w http.ResponseWriter, r *http.Request) {
	var req AddRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		s.writeError(w, "Path is required", http.StatusBadRequest)
		return
	}

	// Validate that it's a git repository
	if !s.gitManager.IsGitRepository(req.Path) {
		s.writeError(w, "Path is not a valid Git repository", http.StatusBadRequest)
		return
	}

	// Generate name from path if not provided
	if req.Name == "" {
		req.Name = filepath.Base(req.Path)
	}

	repo, err := s.stateManager.AddRepository(req.Path, req.Name)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to add repository: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(repo)
}

func (s *Server) removeRepository(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.stateManager.RemoveRepository(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			s.writeError(w, "Repository not found", http.StatusNotFound)
		} else {
			s.writeError(w, fmt.Sprintf("Failed to remove repository: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	statuses, err := s.stateManager.GetAgentStatus()
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get status: %v", err), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(statuses)
}

func (s *Server) handleClaudeWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Note: This endpoint is kept for compatibility but Claude Code
	// actually uses hook mode (--hook flag) with stdin input
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "hook_mode_used"})
}

func (s *Server) handleOpenIDE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Try to open PyCharm via command line for Linux
	err := s.openPyCharmLinux(req.Path)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to open PyCharm: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"status": "opened",
		"path":   req.Path,
	}

	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleDirectorySuggestions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		json.NewEncoder(w).Encode([]DirectorySuggestion{})
		return
	}

	suggestions := s.getDirectorySuggestions(query)
	json.NewEncoder(w).Encode(suggestions)
}

type DirectorySuggestion struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	IsGitRepo   bool   `json:"is_git_repo"`
	HasGitRepos bool   `json:"has_git_repos"`
}

func (s *Server) getDirectorySuggestions(query string) []DirectorySuggestion {
	var suggestions []DirectorySuggestion

	// If query is absolute path, suggest directories from that path
	if strings.HasPrefix(query, "/") {
		suggestions = append(suggestions, s.suggestFromPath(query)...)
	}

	// Add common development directories
	commonPaths := []string{
		"/home",
		"/opt",
		"/usr/local/src",
		"/var/www",
	}

	// Try to get user home directory
	if homeDir, err := os.UserHomeDir(); err == nil {
		commonPaths = append(commonPaths,
			homeDir,
			filepath.Join(homeDir, "dev"),
			filepath.Join(homeDir, "code"),
			filepath.Join(homeDir, "projects"),
			filepath.Join(homeDir, "workspace"),
			filepath.Join(homeDir, "git"),
			filepath.Join(homeDir, "repos"),
		)
	}

	for _, path := range commonPaths {
		if strings.Contains(strings.ToLower(path), strings.ToLower(query)) {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				suggestion := DirectorySuggestion{
					Path:      path,
					Name:      filepath.Base(path),
					Type:      "common",
					IsGitRepo: s.gitManager.IsGitRepository(path),
				}
				suggestion.HasGitRepos = s.hasGitRepositories(path)
				suggestions = append(suggestions, suggestion)
			}
		}
	}

	// Remove duplicates and sort
	seen := make(map[string]bool)
	var unique []DirectorySuggestion
	for _, suggestion := range suggestions {
		if !seen[suggestion.Path] {
			seen[suggestion.Path] = true
			unique = append(unique, suggestion)
		}
	}

	// Sort by relevance (git repos first, then by name)
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].IsGitRepo != unique[j].IsGitRepo {
			return unique[i].IsGitRepo
		}
		if unique[i].HasGitRepos != unique[j].HasGitRepos {
			return unique[i].HasGitRepos
		}
		return unique[i].Path < unique[j].Path
	})

	// Limit results
	if len(unique) > 10 {
		unique = unique[:10]
	}

	return unique
}

func (s *Server) suggestFromPath(query string) []DirectorySuggestion {
	var suggestions []DirectorySuggestion

	// Find the parent directory to scan
	parentDir := filepath.Dir(query)
	if parentDir == query {
		parentDir = "/"
	}

	// Check if parent directory exists
	if _, err := os.Stat(parentDir); err != nil {
		return suggestions
	}

	// Read directory contents
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return suggestions
	}

	baseName := filepath.Base(query)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Filter by query
		if baseName != "" && !strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(baseName)) {
			continue
		}

		fullPath := filepath.Join(parentDir, entry.Name())
		suggestion := DirectorySuggestion{
			Path:      fullPath,
			Name:      entry.Name(),
			Type:      "directory",
			IsGitRepo: s.gitManager.IsGitRepository(fullPath),
		}
		suggestion.HasGitRepos = s.hasGitRepositories(fullPath)

		suggestions = append(suggestions, suggestion)
	}

	return suggestions
}

func (s *Server) hasGitRepositories(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(dir, entry.Name())
		if s.gitManager.IsGitRepository(fullPath) {
			return true
		}
	}

	return false
}

func (s *Server) handleHookStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	repoPath := r.URL.Query().Get("path")
	if repoPath == "" {
		s.writeError(w, "Repository path required", http.StatusBadRequest)
		return
	}

	hookStatus := s.checkHookStatus(repoPath)
	json.NewEncoder(w).Encode(hookStatus)
}

func (s *Server) handleHookInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		s.writeError(w, "Repository path required", http.StatusBadRequest)
		return
	}

	if err := s.installHook(req.Path); err != nil {
		s.writeError(w, fmt.Sprintf("Failed to install hook: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]string{
		"status":  "installed",
		"message": "Claude Code hook configuration installed successfully",
	}

	json.NewEncoder(w).Encode(response)
}

type HookStatus struct {
	Path         string `json:"path"`
	IsInstalled  bool   `json:"is_installed"`
	ConfigPath   string `json:"config_path"`
	HasGitIgnore bool   `json:"has_gitignore"`
}

func (s *Server) checkHookStatus(repoPath string) HookStatus {
	configPath := filepath.Join(repoPath, ".claude", "settings.local.json")
	gitignorePath := filepath.Join(repoPath, ".gitignore")

	isInstalled := s.hasHooksInConfig(configPath)

	_, err := os.Stat(gitignorePath)
	hasGitIgnore := err == nil

	return HookStatus{
		Path:         repoPath,
		IsInstalled:  isInstalled,
		ConfigPath:   configPath,
		HasGitIgnore: hasGitIgnore,
	}
}

func (s *Server) hasHooksInConfig(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}

	hooks, exists := config["hooks"]
	if !exists {
		return false
	}

	hooksMap, ok := hooks.(map[string]interface{})
	if !ok {
		return false
	}

	// Check if any of our expected hooks exist
	for _, event := range []string{"PreToolUse", "PostToolUse", "Stop", "Notification"} {
		if _, exists := hooksMap[event]; exists {
			return true
		}
	}

	return false
}

func (s *Server) installHook(repoPath string) error {
	fmt.Printf("Installing hook for repository: %s\n", repoPath)

	// Validate that the repository path exists
	if _, err := os.Stat(repoPath); err != nil {
		return fmt.Errorf("repository path does not exist: %s", repoPath)
	}

	// Create .claude directory
	claudeDir := filepath.Join(repoPath, ".claude")
	fmt.Printf("Creating directory: %s\n", claudeDir)
	
	// Check if directory already exists
	operation := "create"
	if _, err := os.Stat(claudeDir); err == nil {
		operation = "modify" // Directory already exists, we're modifying its contents
	}
	
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
	}
	
	// Track directory creation
	if operation == "create" {
		s.stateManager.AddAction("file_operation", fmt.Sprintf("📁 Created directory: %s", filepath.Base(claudeDir)))
	}

	// Merge with existing configuration
	configPath := filepath.Join(claudeDir, "settings.local.json")
	fmt.Printf("Updating hook config at: %s\n", configPath)

	if err := s.mergeHookConfig(configPath); err != nil {
		return fmt.Errorf("failed to merge hook config: %w", err)
	}

	// Update .gitignore to exclude .claude
	gitignorePath := filepath.Join(repoPath, ".gitignore")
	fmt.Printf("Updating .gitignore at: %s\n", gitignorePath)
	if err := s.updateGitIgnore(gitignorePath); err != nil {
		return fmt.Errorf("failed to update .gitignore: %w", err)
	}

	fmt.Printf("Hook installation completed successfully for: %s\n", repoPath)
	
	// Broadcast command updates to show file operations
	s.BroadcastStatusUpdate()
	
	return nil
}

func (s *Server) mergeHookConfig(configPath string) error {
	// Check if file exists to determine operation type
	operation := "create"
	if _, err := os.Stat(configPath); err == nil {
		operation = "modify"
	}
	
	// Load existing config or create new one
	var config map[string]interface{}

	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	} else {
		config = make(map[string]interface{})
	}

	// Add hooks to the configuration
	hookConfig := s.generateHookConfig()
	config["hooks"] = hookConfig["hooks"]

	// Write merged configuration
	configData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged config: %w", err)
	}

	err = os.WriteFile(configPath, configData, 0644)
	if err != nil {
		return err
	}
	
	// Track the file operation
	fileName := filepath.Base(configPath)
	if operation == "create" {
		s.stateManager.AddAction("file_operation", fmt.Sprintf("➕ Created file: %s", fileName))
	} else {
		s.stateManager.AddAction("file_operation", fmt.Sprintf("✏️ Modified file: %s", fileName))
	}

	return nil
}

func (s *Server) generateHookConfig() map[string]interface{} {
	// Get the full path to the current executable
	execPath, err := os.Executable()
	if err != nil {
		log.Printf("Failed to get executable path, using relative: %v", err)
		execPath = "coding-agent-dashboard"
	}

	command := fmt.Sprintf("%s --hook", execPath)

	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "*",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"PostToolUse": []map[string]interface{}{
				{
					"matcher": "*",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"Stop": []map[string]interface{}{
				{
					"matcher": "*",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
			"Notification": []map[string]interface{}{
				{
					"matcher": "*",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": command,
						},
					},
				},
			},
		},
	}
}

func (s *Server) updateGitIgnore(gitignorePath string) error {
	claudeEntry := ".claude/"

	// Check if file exists to determine operation type
	operation := "create"
	if _, err := os.Stat(gitignorePath); err == nil {
		operation = "modify"
	}

	// Read existing .gitignore
	var content []byte
	var err error
	if _, err = os.Stat(gitignorePath); err == nil {
		content, err = os.ReadFile(gitignorePath)
		if err != nil {
			return err
		}
	}

	// Check if .claude is already in .gitignore
	contentStr := string(content)
	if strings.Contains(contentStr, claudeEntry) || strings.Contains(contentStr, ".claude") {
		return nil // Already present
	}

	// Add .claude entry
	if len(content) > 0 && !strings.HasSuffix(contentStr, "\n") {
		contentStr += "\n"
	}
	contentStr += "\n# Claude Code configuration (auto-generated)\n"
	contentStr += claudeEntry + "\n"

	err = os.WriteFile(gitignorePath, []byte(contentStr), 0644)
	if err != nil {
		return err
	}
	
	// Track the file operation
	fileName := filepath.Base(gitignorePath)
	if operation == "create" {
		s.stateManager.AddAction("file_operation", fmt.Sprintf("➕ Created file: %s", fileName))
	} else {
		s.stateManager.AddAction("file_operation", fmt.Sprintf("✏️ Modified file: %s", fileName))
	}
	
	return nil
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create channel for this connection
	ch := make(chan string, 10)
	s.hub.AddConnection(ch)

	// Handle connection cleanup
	defer s.hub.RemoveConnection(ch)

	// Send initial status and commands
	statuses, err := s.stateManager.GetAgentStatus()
	if err == nil {
		message := map[string]interface{}{
			"type": "status_update",
			"data": statuses,
		}
		s.hub.Broadcast(message)
	}
	
	// Send initial actions
	actions, err := s.stateManager.GetSystemActions()
	if err == nil {
		// Reverse the slice to show most recent first
		for i, j := 0, len(actions)-1; i < j; i, j = i+1, j-1 {
			actions[i], actions[j] = actions[j], actions[i]
		}
		
		message := map[string]interface{}{
			"type": "actions_update",
			"data": actions,
		}
		s.hub.Broadcast(message)
	}

	// Stream events to client
	for {
		select {
		case message, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(w, message); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) BroadcastStatusUpdate() {
	statuses, err := s.stateManager.GetAgentStatus()
	if err != nil {
		log.Printf("Failed to get status for broadcast: %v", err)
		return
	}

	message := map[string]interface{}{
		"type": "status_update",
		"data": statuses,
	}

	s.hub.Broadcast(message)
	
	// Also broadcast updated actions
	actions, err := s.stateManager.GetSystemActions()
	if err == nil {
		// Reverse the slice to show most recent first
		for i, j := 0, len(actions)-1; i < j; i, j = i+1, j-1 {
			actions[i], actions[j] = actions[j], actions[i]
		}
		
		actionMessage := map[string]interface{}{
			"type": "actions_update",
			"data": actions,
		}
		s.hub.Broadcast(actionMessage)
	}
}

func (s *Server) writeError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}

func (s *Server) openPyCharmLinux(projectPath string) error {
	// Common PyCharm command names on Linux
	commands := []string{
		"pycharm",
		"gtk-launch jetbrains-pycharm",
		"pycharm-professional",
		"pycharm-community",
		"/opt/pycharm/bin/pycharm.sh",
		"/snap/pycharm-professional/current/bin/pycharm.sh",
		"/snap/pycharm-community/current/bin/pycharm.sh",
	}

	// Try each command until one works
	for _, cmd := range commands {
		log.Printf("Trying command: %s", cmd)
		// Split command and arguments
		parts := strings.Fields(cmd)
		if len(parts) == 0 {
			continue
		}
		
		cmdName := parts[0]
		cmdArgs := parts[1:]
		
		// Check if command exists
		if _, err := exec.LookPath(cmdName); err == nil {
			log.Printf("Found command: %s", cmdName)
			// Execute the command with its arguments plus the project path
			args := append(cmdArgs, projectPath)
			log.Printf("Executing command: %s with args: %v", cmdName, args)
			
			execCmd := exec.Command(cmdName, args...)
			err := execCmd.Start() // Use Start() instead of Run() to not wait for completion
			
			// Add action to UI display AFTER successful execution
			if err == nil {
				go func() {
					log.Printf("Adding action to UI")
					description := "🚀 Opened PyCharm"
					command := fmt.Sprintf("%s %s", cmdName, strings.Join(args, " "))
					s.stateManager.AddActionWithCommand("command", description, command)
					log.Printf("Action added, broadcasting update")
					s.BroadcastStatusUpdate()
					log.Printf("Broadcast completed")
				}()
			}
			
			if err != nil {
				log.Printf("Command execution failed: %v", err)
				return fmt.Errorf("failed to start PyCharm: %v", err)
			}
			
			log.Printf("Successfully launched PyCharm with command: %s %s", cmd, projectPath)
			
			return nil
		}
	}

	return fmt.Errorf("PyCharm not found. Tried commands: %v", commands)
}

func (s *Server) handleMinionMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MinionMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		s.writeError(w, "Path is required", http.StatusBadRequest)
		return
	}

	if req.Message == "" {
		s.writeError(w, "Message is required", http.StatusBadRequest)
		return
	}

	// Add message to the minion queue for this path
	log.Printf("Web API: Adding minion message for path '%s': %s", req.Path, req.Message)
	if err := s.stateManager.AddMinionMessage(req.Path, req.Message); err != nil {
		s.writeError(w, fmt.Sprintf("Failed to send message to minion: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("Web API: Successfully added minion message for path '%s'", req.Path)

	response := map[string]string{
		"status":  "sent",
		"path":    req.Path,
		"message": req.Message,
	}

	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleSystemCommands(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	actions, err := s.stateManager.GetSystemActions()
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get system actions: %v", err), http.StatusInternalServerError)
		return
	}

	// Reverse the slice to show most recent first
	for i, j := 0, len(actions)-1; i < j; i, j = i+1, j-1 {
		actions[i], actions[j] = actions[j], actions[i]
	}

	json.NewEncoder(w).Encode(actions)
}
