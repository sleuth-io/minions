package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	
	"coding-agent-dashboard/internal/git"
	"coding-agent-dashboard/internal/state"
)

type Server struct {
	stateManager *state.Manager
	gitManager   *git.Manager
}

type AddRepositoryRequest struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func NewServer(stateManager *state.Manager, gitManager *git.Manager) *Server {
	return &Server{
		stateManager: stateManager,
		gitManager:   gitManager,
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
	
	// TODO: Implement Claude webhook handling
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "received"})
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
	
	// Generate PyCharm URL
	ideURL := fmt.Sprintf("pycharm://open?file=%s", req.Path)
	
	response := map[string]string{
		"url": ideURL,
		"status": "generated",
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
		"status": "installed",
		"message": "Claude Code hook configuration installed successfully",
	}
	
	json.NewEncoder(w).Encode(response)
}

type HookStatus struct {
	Path        string `json:"path"`
	IsInstalled bool   `json:"is_installed"`
	ConfigPath  string `json:"config_path"`
	HasGitIgnore bool  `json:"has_gitignore"`
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
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude directory: %w", err)
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
	return nil
}

func (s *Server) mergeHookConfig(configPath string) error {
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
	
	return os.WriteFile(configPath, configData, 0644)
}

func (s *Server) generateHookConfig() map[string]interface{} {
	return map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []map[string]interface{}{
				{
					"matcher": "*",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": "coding-agent-dashboard --hook",
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
							"command": "coding-agent-dashboard --hook",
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
							"command": "coding-agent-dashboard --hook",
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
							"command": "coding-agent-dashboard --hook",
						},
					},
				},
			},
		},
	}
}

func (s *Server) updateGitIgnore(gitignorePath string) error {
	claudeEntry := ".claude/"
	
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
	
	return os.WriteFile(gitignorePath, []byte(contentStr), 0644)
}

func (s *Server) writeError(w http.ResponseWriter, message string, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}