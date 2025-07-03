# Sleuth Minions - Coding Agent Dashboard

A lightweight dashboard application to monitor and manage ongoing coding agent activities across multiple Git repositories, with advanced minion mode for transparent subprocess execution and real-time message injection.

## Overview

Sleuth Minions provides real-time visibility into active coding agent sessions while offering a unique "minion mode" that enables transparent subprocess execution with message injection capabilities. The application serves as both a monitoring dashboard and a sophisticated process wrapper for AI coding assistants like Claude Code.

## Core Features

### 1. Repository Management
- **Multi-repository monitoring**: Track all active projects from a centralized dashboard
- **Git worktree discovery**: Automatically discover and display all worktrees for each repository
- **Real-time status tracking**: Monitor Claude Code instance status per directory
- **Quick IDE access**: One-click PyCharm integration for seamless development transitions

### 2. Advanced Minion Mode
- **Transparent subprocess execution**: Run any command (like Claude Code) while maintaining full input/output transparency
- **Real-time message injection**: Send commands to running processes via web UI
- **PTY-based terminal emulation**: Full terminal compatibility with proper key handling
- **State-based messaging**: Persistent message queue system for reliable communication

### 3. Web Dashboard
- **Live status monitoring**: Real-time updates via WebSocket connections
- **Message expansion interface**: Click to expand and interact with agent messages
- **Interactive controls**: Send messages directly to running agents via "Hi" button and expandable UI
- **Repository overview**: Comprehensive view of all monitored repositories and their worktrees

## How It Works

### Dashboard Mode
```bash
./sleuth-minions --port 8030
```
Starts the web dashboard on the specified port, providing:
- Repository and worktree monitoring
- Claude Code status tracking via webhooks
- Real-time updates through Server-Sent Events
- Message queue management interface

### Minion Mode
```bash
./sleuth-minions --minion claude
```
Wraps the Claude process with advanced capabilities:
- **Transparent I/O**: All Claude output passes through unchanged
- **Message injection**: Accepts messages from web UI and injects them as user input
- **PTY emulation**: Creates a pseudo-terminal for proper key sequence handling
- **Character-by-character simulation**: Mimics human typing for authentic input recognition

### Hook Mode
```bash
./sleuth-minions --hook
```
Integrates with Claude Code's webhook system for status tracking.

## Architecture

### Technical Stack
- **Backend**: Go with embedded web server and PTY management
- **Frontend**: Vue.js SPA with real-time WebSocket updates
- **State Management**: JSON files in platform-appropriate directories
- **Process Management**: Advanced PTY-based subprocess control

### Key Components

#### Minion Mode Innovation
The minion mode implements sophisticated subprocess wrapping:

1. **PTY Creation**: Uses `github.com/creack/pty` to create pseudo-terminals
2. **Raw Terminal Mode**: Puts real terminal in raw mode for proper key forwarding
3. **Message Queue System**: File-based message persistence with atomic operations
4. **Character Simulation**: Types injected messages character-by-character with delays

#### State Management
- `repositories.json`: Configured Git repositories
- `agent-status.json`: Current Claude Code instance states
- `minion-messages/`: Directory containing message files for each minion instance
- Debug logging: `/tmp/minion-debug.log` for troubleshooting

#### Web Interface
- **Message Expansion**: Click messages to expand and see full content
- **Action Buttons**: Send pre-defined messages ("Hi") or custom commands
- **Real-time Updates**: Live status changes via WebSocket connections
- **Repository Tree**: Hierarchical view of repositories and worktrees

## API Endpoints

### Core Dashboard APIs
- `GET /api/repositories`: List configured repositories
- `POST /api/repositories`: Add new repository
- `DELETE /api/repositories/{id}`: Remove repository
- `GET /api/status`: Get all Claude Code statuses

### Minion Communication
- `POST /api/minion/message`: Send message to minion process
  ```json
  {
    "path": "/working/directory",
    "message": "hi"
  }
  ```

### Status Updates
- `POST /api/webhook/claude`: Receive Claude Code status updates
- WebSocket endpoint for real-time dashboard updates

## Installation & Usage

### Prerequisites
```bash
go get github.com/creack/pty
go get golang.org/x/term
```

### Build
```bash
go build -o sleuth-minions
```

### Usage Examples

#### Start Dashboard
```bash
./sleuth-minions --port 8030
```
Open browser to `http://localhost:8030`

#### Run Claude in Minion Mode
```bash
./sleuth-minions --minion claude
```
- Claude runs normally with full terminal functionality
- Web dashboard can send messages via "Hi" button
- All I/O is transparent and real-time

#### Add Repositories
1. Open web dashboard
2. Click "Add Repository"
3. Enter Git repository path
4. Monitor all worktrees automatically

## Advanced Features

### Message Injection Technical Details
The minion mode implements sophisticated input simulation:

```go
// Character-by-character typing simulation
for _, char := range message.Message {
    stdinPipe.Write([]byte{byte(char)})
    time.Sleep(10 * time.Millisecond)
}
// Send Enter key
stdinPipe.Write([]byte{'\r'})
```

This approach ensures Claude recognizes injected messages as authentic user input.

### PTY Terminal Emulation
- **Raw mode terminal**: Proper forwarding of special keys (arrows, enter, etc.)
- **Terminal size synchronization**: Matches parent terminal dimensions
- **Signal handling**: Proper cleanup and terminal restoration

### State Persistence
- **Atomic file operations**: Prevents corruption during concurrent access
- **Path-based message routing**: Each working directory has its own message queue
- **Safe filename generation**: Handles special characters in directory paths

## Configuration

### Config Directory Locations
- **Linux/macOS**: `~/.config/coding-agent-dashboard/`
- **Windows**: `%APPDATA%/coding-agent-dashboard/`

### Debug Logging
Minion processes log to `/tmp/minion-debug.log`:
```
[19:37:26] Started process: claude (PID: 2619659)
[19:37:26] Found message: hi
[19:37:26] Successfully sent message 'hi' + Enter to stdin
```

## Use Cases

### Development Workflow
1. **Start dashboard**: Monitor all active projects
2. **Run Claude in minion mode**: Get transparent AI assistance
3. **Send commands via web UI**: Control Claude without switching contexts
4. **Quick IDE access**: One-click project opening

### Team Coordination
- **Shared visibility**: See which projects have active AI assistance
- **Status tracking**: Monitor AI agent activity across repositories
- **Remote interaction**: Send commands to running agents

### Process Automation
- **Webhook integration**: Automatic status updates
- **Message queuing**: Reliable command delivery
- **State persistence**: Survives process restarts

## Troubleshooting

### Common Issues

#### No Claude Output
- Ensure terminal stdin is properly configured
- Check `/tmp/minion-debug.log` for process startup messages
- Verify PTY creation succeeded

#### Message Injection Not Working
- Confirm process is running: check PID in debug log
- Verify working directory matches web UI path
- Check message file creation in `~/.config/coding-agent-dashboard/minion-messages/`

#### Terminal Key Issues
- Ensure raw mode is enabled (automatic in minion mode)
- Check terminal size synchronization
- Verify PTY setup completed successfully

### Debug Commands
```bash
# Check message files
ls ~/.config/coding-agent-dashboard/minion-messages/

# Monitor debug log
tail -f /tmp/minion-debug.log

# Test message injection
curl -X POST http://localhost:8030/api/minion/message \
  -H "Content-Type: application/json" \
  -d '{"path": "/your/working/dir", "message": "test"}'
```

## Future Enhancements

### Dashboard Features
- Multi-IDE support beyond PyCharm
- Advanced Claude Code control (start/stop/configure)
- Repository metrics and analytics
- Team collaboration features

### Minion Mode Extensions
- Custom message templates
- Macro recording and playback
- Multi-process message broadcasting
- Advanced terminal feature support

### Integration Possibilities
- CI/CD pipeline integration
- Slack/Discord notifications
- Custom webhook endpoints
- Plugin architecture

## Contributing

The project demonstrates advanced Go subprocess management, real-time web interfaces, and sophisticated terminal emulation. Key areas for contribution:

- Enhanced PTY feature support
- Additional AI agent integrations
- Advanced message routing
- Cross-platform compatibility improvements

---

**Note**: This application showcases cutting-edge techniques for transparent subprocess control and real-time web-based process interaction, making it valuable for both practical use and as a reference implementation for advanced Go process management.