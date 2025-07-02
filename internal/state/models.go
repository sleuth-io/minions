package state

import "time"

type Repository struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	IsMain bool   `json:"is_main"`
}

type AgentStatus struct {
	Path         string    `json:"path"`
	Status       string    `json:"status"` // running, idle, paused, error
	LastActivity time.Time `json:"last_activity"`
	PID          int       `json:"pid,omitempty"`
}

type RepositoryWithWorktrees struct {
	Repository
	Worktrees []Worktree    `json:"worktrees"`
	Status    []AgentStatus `json:"status"`
}