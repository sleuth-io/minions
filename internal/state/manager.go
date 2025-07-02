package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Manager struct {
	configDir string
}

func NewManager(configDir string) (*Manager, error) {
	return &Manager{
		configDir: configDir,
	}, nil
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

func (m *Manager) SaveAgentStatus(statuses []AgentStatus) error {
	statusFile := filepath.Join(m.configDir, "agent-status.json")
	
	data, err := json.MarshalIndent(statuses, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent status: %w", err)
	}
	
	if err := os.WriteFile(statusFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent status file: %w", err)
	}
	
	return nil
}