# Claude Code Hooks Integration

This document explains how to integrate the Coding Agent Dashboard with Claude Code hooks to automatically track session status.

## What Hooks Do

The dashboard uses Claude Code hooks to:
- Track when Claude Code sessions start/stop
- Monitor tool usage activity  
- Update repository status in real-time
- Provide accurate "running", "idle", or "unknown" states

## Setup Instructions

### 1. Build the Dashboard

First, make sure the dashboard is built and available in your PATH:

```bash
make build
sudo cp coding-agent-dashboard /usr/local/bin/
# OR add the current directory to your PATH
export PATH=$PATH:$(pwd)
```

### 2. Configure Claude Code Hooks

Add the hooks configuration to your Claude Code settings. The location depends on your OS:

**Linux/macOS**: `~/.config/claude-code/settings.json`
**Windows**: `%APPDATA%\claude-code\settings.json`

Merge the contents of `claude-hooks-config.json` into your settings file, or if you don't have a settings file yet, copy it directly.

### 3. Verify Hook Integration

Start a Claude Code session in a directory that's configured in your dashboard:

```bash
cd /path/to/your/repository
claude-code
```

The dashboard should now show the repository status as "running" when Claude is active, and "idle" when the session ends.

## How It Works

### Hook Events Tracked

- **PreToolUse**: Triggered before Claude runs any tool (sets status to "running")
- **PostToolUse**: Triggered after tool completion (keeps status as "running") 
- **Stop**: Triggered when Claude session ends (sets status to "idle")
- **Notification**: Triggered on Claude notifications (keeps status as "running")

### Status Flow

1. **unknown** → Default state when no hook data exists
2. **running** → When Claude is actively using tools or sending notifications
3. **idle** → When Claude session has ended (Stop event)

### Data Stored

Hook events update the `agent-status.json` file with:
- `path`: Working directory where Claude is running
- `status`: Current state (running/idle/unknown)
- `last_activity`: Timestamp of last hook event
- `pid`: Process ID (for debugging)

## Troubleshooting

### Hooks Not Working

1. **Check binary location**: Make sure `coding-agent-dashboard` is in your PATH
2. **Test hook manually**: 
   ```bash
   echo '{"event":"PreToolUse","working_dir":"/test"}' | coding-agent-dashboard --hook
   ```
3. **Check permissions**: Ensure the binary is executable
4. **Verify config**: Make sure the hooks configuration is valid JSON

### Status Not Updating

1. **Check config directory**: Look for `agent-status.json` in `~/.config/coding-agent-dashboard/`
2. **Run dashboard with debug**: Check console output for hook activity
3. **Verify repository paths**: Make sure the working directory matches a configured repository

### Performance Concerns

The hooks are designed to be lightweight:
- Each hook call takes <10ms typically
- Only updates JSON files, no network calls
- Minimal data processing

## Configuration Customization

You can customize which events to track by modifying the hooks configuration:

- Remove events you don't need
- Add tool-specific matchers instead of "*" wildcard
- Adjust the input data passed to hooks

Example for only tracking specific tools:
```json
{
  "PreToolUse": [
    {
      "matcher": "Edit|Write|Bash",
      "hooks": [...]
    }
  ]
}
```