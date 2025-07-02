package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	
	"coding-agent-dashboard/internal/state"
)

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

func (g *Manager) IsGitRepository(path string) bool {
	gitDir := filepath.Join(path, ".git")
	_, err := os.Stat(gitDir)
	return err == nil
}

func (g *Manager) GetWorktrees(repoPath string) ([]state.Worktree, error) {
	if !g.IsGitRepository(repoPath) {
		return nil, fmt.Errorf("not a git repository: %s", repoPath)
	}
	
	// Get main repository info
	mainBranch, err := g.getCurrentBranch(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get main branch: %w", err)
	}
	
	worktrees := []state.Worktree{
		{
			Path:   repoPath,
			Branch: mainBranch,
			IsMain: true,
		},
	}
	
	// Get worktrees using git worktree list
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// If git worktree list fails, just return the main worktree
		return worktrees, nil
	}
	
	// Parse worktree output
	lines := strings.Split(string(output), "\n")
	var currentWorktree *state.Worktree
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if currentWorktree != nil && !currentWorktree.IsMain {
				worktrees = append(worktrees, *currentWorktree)
			}
			currentWorktree = nil
			continue
		}
		
		if strings.HasPrefix(line, "worktree ") {
			path := strings.TrimPrefix(line, "worktree ")
			if path != repoPath { // Skip main worktree as we already added it
				currentWorktree = &state.Worktree{
					Path:   path,
					IsMain: false,
				}
			}
		} else if strings.HasPrefix(line, "branch ") && currentWorktree != nil {
			branch := strings.TrimPrefix(line, "branch ")
			branch = strings.TrimPrefix(branch, "refs/heads/")
			currentWorktree.Branch = branch
		}
	}
	
	// Add the last worktree if exists
	if currentWorktree != nil && !currentWorktree.IsMain {
		worktrees = append(worktrees, *currentWorktree)
	}
	
	return worktrees, nil
}

func (g *Manager) getCurrentBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	
	return strings.TrimSpace(string(output)), nil
}