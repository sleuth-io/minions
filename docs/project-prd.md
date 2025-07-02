# Coding Agent Dashboard MVP - Product Requirements Document

## Overview
A lightweight dashboard application to monitor and manage ongoing coding agent activities across multiple Git repositories. The app provides visibility into Claude Code instances, Git worktrees, and quick access to development environments.

## Goals
- Provide real-time visibility into active coding agent sessions
- Enable quick management of multiple Git repositories and worktrees
- Offer convenient IDE integration for development workflows
- Maintain minimal system overhead with filesystem-based state management

## Target Users
- Developers using Claude Code across multiple projects
- Teams coordinating AI-assisted development efforts
- Individual developers managing complex multi-repository workflows

## Core Features

### 1. Repository Management
**User Story**: As a developer, I want to configure which repositories to monitor so I can track all my active projects.

**Acceptance Criteria**:
- Display list of configured Git repositories on main page
- Add new repositories via file path input
- Remove repositories from monitoring list
- Validate that added paths are valid Git repositories
- Persist repository list to filesystem

### 2. Worktree Discovery & Display
**User Story**: As a developer, I want to see all worktrees for each repository so I can understand my parallel development contexts.

**Acceptance Criteria**:
- Automatically discover Git worktrees for each configured repository
- Display main repository directory and all associated worktrees
- Show worktree branch information
- Refresh worktree list on demand

### 3. Claude Code Status Tracking
**User Story**: As a developer, I want to see which directories have active Claude Code sessions so I can avoid conflicts and understand current AI activity.

**Acceptance Criteria**:
- Track Claude Code instance status per directory (main repo + worktrees)
- Display status indicators (running, paused, idle, error)
- Receive status updates via webhook integration
- Show timestamp of last activity

### 4. IDE Integration
**User Story**: As a developer, I want to quickly open any directory in PyCharm so I can seamlessly transition between monitoring and development.

**Acceptance Criteria**:
- Provide "Open in PyCharm" action for each directory
- Generate proper IDE protocol URLs (e.g., `pycharm://open?file=/path/to/directory`)
- Support browser-based IDE launching

### 5. Claude Hooks Integration
**User Story**: As a system administrator, I want the app to integrate with Claude hooks so it can participate in the Claude Code workflow.

**Acceptance Criteria**:
- Support `--hook` command line flag
- Skip web UI startup when running in hook mode
- Execute appropriate webhook notifications
- Return proper exit codes for hook integration

## Technical Requirements

### Architecture
- **Backend**: Single Go binary with embedded web server
- **Frontend**: Vue.js SPA served from Go binary
- **State Management**: JSON files in platform-appropriate directories
- **Communication**: HTTP APIs + WebSocket for real-time updates

### Platform Support
- Cross-platform compatibility (Windows, macOS, Linux)
- Use OS-appropriate config directories:
  - Linux/macOS: `~/.config/coding-agent-dashboard/`
  - Windows: `%APPDATA%/coding-agent-dashboard/`

### Data Persistence
- `repositories.json`: List of configured Git repositories
- `agent-status.json`: Current Claude Code instance states
- `settings.json`: Application configuration

### API Endpoints
- `GET /api/repositories`: List configured repositories
- `POST /api/repositories`: Add new repository
- `DELETE /api/repositories/{id}`: Remove repository
- `GET /api/status`: Get all Claude Code statuses
- `POST /api/webhook/claude`: Receive Claude Code status updates
- `POST /api/actions/open-ide`: Trigger IDE opening

## Non-Functional Requirements

### Performance
- Startup time < 2 seconds
- Repository scanning < 5 seconds for typical projects
- Minimal memory footprint (< 50MB)

### Reliability
- Graceful handling of repository access errors
- Automatic recovery from corrupted state files
- Proper cleanup of resources on shutdown

### Security
- Webhook endpoint authentication (shared secret)
- Input validation for all file paths
- Safe handling of Git operations

## MVP Scope Limitations

### Included
- Basic repository and worktree management
- Claude Code status tracking via webhooks
- PyCharm IDE integration
- Simple web UI for monitoring
- Claude hooks integration

### Explicitly Excluded (Future Versions)
- Multiple IDE support beyond PyCharm
- Advanced Claude Code control (start/stop/configure)
- Repository metrics and analytics
- Team collaboration features
- Configuration import/export
- Detailed logging and debugging UI

## User Interface Mockup

### Main Dashboard
```
Coding Agent Dashboard

[+ Add Repository]

Repository: /path/to/project1
├── main (running) [Open in PyCharm]
├── feature/auth (idle) [Open in PyCharm]
└── hotfix/bug-123 (paused) [Open in PyCharm]

Repository: /path/to/project2
├── main (idle) [Open in PyCharm]
└── develop (running) [Open in PyCharm]
```

## Success Metrics
- Successfully tracks Claude Code status across multiple repositories
- Enables quick IDE access with single click
- Maintains accurate state persistence
- Integrates properly with Claude hooks workflow
- Provides stable, responsive web interface

## Implementation Priority

### Phase 1 (Core MVP)
1. Basic Go web server with Vue frontend
2. Repository configuration management
3. Git worktree discovery
4. Filesystem state persistence

### Phase 2 (Integration)
1. Claude hooks integration
2. Webhook endpoint for status updates
3. PyCharm protocol URL generation

### Phase 3 (Polish)
1. UI refinements and error handling
2. Performance optimization
3. Cross-platform testing and packaging