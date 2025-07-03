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
	Path            string    `json:"path"`
	Status          string    `json:"status"` // running, idle, paused, error
	LastActivity    time.Time `json:"last_activity"`
	PID             int       `json:"pid,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	TranscriptPath  string    `json:"transcript_path,omitempty"`
	LastMessage     string    `json:"last_message,omitempty"`
	FullLastMessage string    `json:"full_last_message,omitempty"`
}

type RepositoryWithWorktrees struct {
	Repository
	Worktrees []Worktree    `json:"worktrees"`
	Status    []AgentStatus `json:"status"`
}