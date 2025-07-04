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
	Path           string    `json:"path"`
	Status         string    `json:"status"` // running, idle, paused, error
	LastActivity   time.Time `json:"last_activity"`
	PID            int       `json:"pid,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	TranscriptPath string    `json:"transcript_path,omitempty"`
}

// AgentStatusWithMessages is used for API responses that include last messages from memory
type AgentStatusWithMessages struct {
	AgentStatus
	LastMessage     string `json:"last_message,omitempty"`
	FullLastMessage string `json:"full_last_message,omitempty"`
}

type RepositoryWithWorktrees struct {
	Repository
	Worktrees []Worktree                `json:"worktrees"`
	Status    []AgentStatusWithMessages `json:"status"`
}

type MinionMessage struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`      // Working directory of the minion
	Message   string    `json:"message"`   // Message to send to stdin
	Timestamp time.Time `json:"timestamp"`
}

type SystemAction struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`        // "command", "file_operation", etc.
	Description string    `json:"description"` // Human readable description
	Command     string    `json:"command,omitempty"` // Optional actual command text
	Timestamp   time.Time `json:"timestamp"`
}