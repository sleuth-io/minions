package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TranscriptParser handles parsing Claude Code transcript files
type TranscriptParser struct{}

// NewTranscriptParser creates a new transcript parser
func NewTranscriptParser() *TranscriptParser {
	return &TranscriptParser{}
}

// MessageInfo contains information about the last message or tool call request
type MessageInfo struct {
	Content     string
	IsToolCall  bool
	ToolName    string
	ToolAction  string
}

// TranscriptEntry represents a single entry in a Claude Code transcript
type TranscriptEntry struct {
	Raw       map[string]interface{}
	Message   *MessageData
	Timestamp time.Time
	Type      string
	Role      string
}

// MessageData contains parsed message information
type MessageData struct {
	Role    string
	Content string
}

// TranscriptIterator provides efficient iteration over transcript entries
type TranscriptIterator struct {
	parser  *TranscriptParser
	file    *os.File
	scanner *bufio.Scanner
}

// TranscriptInfo contains information about a transcript file
type TranscriptInfo struct {
	Path      string
	SessionID string
}

// IterateTranscript creates an iterator for efficiently reading transcript entries
func (tp *TranscriptParser) IterateTranscript(transcriptPath string) (*TranscriptIterator, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	
	return &TranscriptIterator{
		parser:  tp,
		file:    file,
		scanner: bufio.NewScanner(file),
	}, nil
}

// Next returns the next transcript entry or nil if no more entries
func (ti *TranscriptIterator) Next() (*TranscriptEntry, error) {
	for ti.scanner.Scan() {
		line := strings.TrimSpace(ti.scanner.Text())
		if line == "" {
			continue
		}
		
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}
		
		transcriptEntry := &TranscriptEntry{
			Raw: entry,
		}
		
		// Parse timestamp
		if timestampStr, ok := entry["timestamp"].(string); ok {
			if timestamp, err := time.Parse(time.RFC3339, timestampStr); err == nil {
				transcriptEntry.Timestamp = timestamp
			}
		}
		
		// Parse type
		if typeStr, ok := entry["type"].(string); ok {
			transcriptEntry.Type = typeStr
		}
		
		// Parse message if present
		if message, ok := entry["message"]; ok {
			if msgMap, ok := message.(map[string]interface{}); ok {
				if role, ok := msgMap["role"].(string); ok {
					transcriptEntry.Role = role
					content := ti.parser.extractMessageContent(msgMap, role)
					if content != "" {
						transcriptEntry.Message = &MessageData{
							Role:    role,
							Content: content,
						}
					}
				}
			}
		}
		
		return transcriptEntry, nil
	}
	
	return nil, ti.scanner.Err()
}

// Close closes the transcript iterator and underlying file
func (ti *TranscriptIterator) Close() error {
	return ti.file.Close()
}

// extractMessageContent extracts text content from a message based on role
func (tp *TranscriptParser) extractMessageContent(msgMap map[string]interface{}, role string) string {
	if role == "user" {
		if content, ok := msgMap["content"].(string); ok {
			return content
		}
	} else if role == "assistant" {
		// Assistant messages have content as array of objects
		if contentArray, ok := msgMap["content"].([]interface{}); ok {
			var textParts []string
			for _, item := range contentArray {
				if contentObj, ok := item.(map[string]interface{}); ok {
					if contentType, ok := contentObj["type"].(string); ok && contentType == "text" {
						if text, ok := contentObj["text"].(string); ok {
							textParts = append(textParts, text)
						}
					}
				}
			}
			return strings.Join(textParts, " ")
		}
	}
	return ""
}

// FindLastConversationalMessage finds the most recent user or assistant message
func (tp *TranscriptParser) FindLastConversationalMessage(transcriptPath string) (*TranscriptEntry, error) {
	iterator, err := tp.IterateTranscript(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	
	var lastEntry *TranscriptEntry
	
	for {
		entry, err := iterator.Next()
		if err != nil {
			return nil, err
		}
		if entry == nil {
			break
		}
		
		// Keep track of the last conversational message
		if entry.Message != nil && (entry.Role == "user" || entry.Role == "assistant") {
			lastEntry = entry
		}
	}
	
	return lastEntry, nil
}

// readLinesFromEnd reads the last N lines from a file efficiently
func (tp *TranscriptParser) readLinesFromEnd(file *os.File, maxLines int) ([]string, error) {
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	fileSize := stat.Size()
	if fileSize == 0 {
		return []string{}, nil
	}

	// Estimate bytes per line (reasonable guess for JSON lines)
	estimatedBytesPerLine := 500
	readSize := int64(maxLines * estimatedBytesPerLine)
	
	// Don't read more than the file size
	if readSize > fileSize {
		readSize = fileSize
	}
	
	// Seek to the estimated position
	startPos := fileSize - readSize
	if startPos < 0 {
		startPos = 0
	}
	
	_, err = file.Seek(startPos, 0)
	if err != nil {
		return nil, err
	}
	
	scanner := bufio.NewScanner(file)
	var lines []string
	
	// If we didn't start at the beginning, skip the first potentially partial line
	if startPos > 0 {
		scanner.Scan()
	}
	
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	
	// Keep only the last maxLines
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	
	return lines, scanner.Err()
}

// GetLastMessage extracts the last conversational message or tool call request from a Claude Code transcript
func (tp *TranscriptParser) GetLastMessage(transcriptPath string) (string, error) {
	messageInfo, err := tp.GetLastMessageInfo(transcriptPath)
	if err != nil {
		return "", err
	}
	
	if messageInfo.IsToolCall {
		// Format tool call request for display
		return fmt.Sprintf("🔧 Tool permission requested: %s", messageInfo.ToolAction), nil
	}
	
	// Create truncated version for display
	if len(messageInfo.Content) > 200 {
		return messageInfo.Content[:200] + "...", nil
	}
	return messageInfo.Content, nil
}

// GetLastMessageFull extracts the full last conversational message from a Claude Code transcript
func (tp *TranscriptParser) GetLastMessageFull(transcriptPath string) (string, error) {
	iterator, err := tp.IterateTranscript(transcriptPath)
	if err != nil {
		return "", err
	}
	defer iterator.Close()

	var lastUserMessage, lastAssistantMessage string

	for {
		entry, err := iterator.Next()
		if err != nil {
			return "", err
		}
		if entry == nil {
			break
		}

		// Process conversational messages
		if entry.Message != nil && (entry.Role == "user" || entry.Role == "assistant") {
			// Clean content by removing tool calls
			cleanContent := tp.cleanMessageContent(entry.Message.Content)
			if cleanContent != "" && !tp.IsSystemOutput(cleanContent) {
				if entry.Role == "assistant" {
					lastAssistantMessage = cleanContent
				} else if entry.Role == "user" {
					lastUserMessage = cleanContent
				}
			}
		}
	}

	// Prefer assistant messages (responses to user) over user messages
	result := lastAssistantMessage
	if result == "" {
		result = lastUserMessage
	}

	return result, nil
}

// GetLastMessageInfo extracts detailed information about the last message or tool call request
func (tp *TranscriptParser) GetLastMessageInfo(transcriptPath string) (*MessageInfo, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Use our existing readLinesFromEnd function which works reliably
	lines, err := tp.readLinesFromEnd(file, 50) // Read last 50 lines
	if err != nil {
		return nil, err
	}

	// Process lines in reverse order (most recent first)
	for i := len(lines) - 1; i >= 0; i-- {
		lineStr := strings.TrimSpace(lines[i])
		if lineStr == "" {
			continue
		}

		// Try to parse this line for a valid message
		if messageInfo := tp.parseLineForMessage(lineStr); messageInfo != nil {
			return messageInfo, nil
		}
	}

	// No valid message found
	return &MessageInfo{Content: "", IsToolCall: false}, nil
}

// GetLastUserMessageTime finds the timestamp of the most recent user message
func (tp *TranscriptParser) GetLastUserMessageTime(transcriptPath string) (time.Time, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return time.Time{}, err
	}
	defer file.Close()

	// Read from the end of the file, line by line
	lines, err := tp.readLinesFromEnd(file, 50) // Read last 50 lines, should be enough for recent messages
	if err != nil {
		return time.Time{}, err
	}

	// Process lines to find the most recent user message
	var lastUserTime time.Time
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}

		// Check if this is a user message using Claude Code transcript format
		if typeField, ok := entry["type"].(string); ok && typeField == "user" {
			// Try to parse the timestamp
			if timestampStr, ok := entry["timestamp"].(string); ok {
				if timestamp, err := time.Parse(time.RFC3339, timestampStr); err == nil {
					if timestamp.After(lastUserTime) {
						lastUserTime = timestamp
					}
				}
			}
		}
	}
	return lastUserTime, nil
}

// cleanMessageContent removes tool calls and cleans up whitespace
func (tp *TranscriptParser) cleanMessageContent(content string) string {
	// Remove tool call blocks like <function_calls>...</function_calls>
	toolCallRegex := regexp.MustCompile(`(?s)<function_calls>.*?</function_calls>`)
	content = toolCallRegex.ReplaceAllString(content, "")

	// Remove empty lines and trim
	lines := strings.Split(content, "\n")
	var cleanLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleanLines = append(cleanLines, trimmed)
		}
	}

	result := strings.Join(cleanLines, " ")
	return strings.TrimSpace(result)
}

// IsSystemOutput checks if content looks like system/hook output rather than conversation
func (tp *TranscriptParser) IsSystemOutput(content string) bool {
	// Check for patterns that indicate system/hook output rather than conversation
	systemPatterns := []string{
		"Config directory:",
		"Updated agent status:",
		"completed successfully:",
		"[1m", // ANSI color codes
		"[/home/", // Path patterns in system output
		"--hook",
		"Stop [",
		"___go_build_",
	}

	contentLower := strings.ToLower(content)
	for _, pattern := range systemPatterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// truncateString helper for debug logging
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}


// parseLineForMessage parses a single line and returns MessageInfo if it's a valid conversational message
func (tp *TranscriptParser) parseLineForMessage(line string) *MessageInfo {
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil // Skip malformed lines
	}


	// Check for tool permission requests
	if entryType, ok := entry["type"].(string); ok {
		if strings.Contains(entryType, "permission") || strings.Contains(entryType, "tool_request") {
			if message, ok := entry["message"].(map[string]interface{}); ok {
				if toolName, ok := message["tool_name"].(string); ok {
					return &MessageInfo{
						Content:    fmt.Sprintf("Permission requested to use %s tool", toolName),
						IsToolCall: true,
						ToolName:   toolName,
						ToolAction: fmt.Sprintf("Use %s tool", toolName),
					}
				}
			}
		}
	}

	// Check if this entry has a message field
	if message, ok := entry["message"].(map[string]interface{}); ok {
		if role, ok := message["role"].(string); ok && (role == "user" || role == "assistant") {
			var content string

			if role == "user" {
				// User messages have content as string or array for tool results
				if userContent, ok := message["content"].(string); ok {
					content = userContent
				} else if contentArray, ok := message["content"].([]interface{}); ok {
					// Skip tool results entirely
					for _, item := range contentArray {
						if contentObj, ok := item.(map[string]interface{}); ok {
							if contentType, ok := contentObj["type"].(string); ok && contentType == "tool_result" {
								return nil // Skip tool results
							}
						}
					}
				}
			} else if role == "assistant" {
				// Assistant messages have content as array of objects
				if contentArray, ok := message["content"].([]interface{}); ok {
					var textParts []string
					
					for _, item := range contentArray {
						if contentObj, ok := item.(map[string]interface{}); ok {
							if contentType, ok := contentObj["type"].(string); ok && contentType == "text" {
								if text, ok := contentObj["text"].(string); ok {
									textParts = append(textParts, text)
								}
							}
						}
					}
					
					if len(textParts) > 0 {
						content = strings.Join(textParts, " ")
					}
				}
			}

			if content != "" {
				// Clean content by removing tool calls
				cleanContent := tp.cleanMessageContent(content)
				if cleanContent != "" && !tp.IsSystemOutput(cleanContent) {
					return &MessageInfo{
						Content:    cleanContent,
						IsToolCall: false,
						ToolName:   "",
						ToolAction: "",
					}
				}
			}
		}
	}

	return nil
}

// FindMostRecentTranscript finds the most recently modified transcript file for a project path
func (tp *TranscriptParser) FindMostRecentTranscript(projectPath string) (*TranscriptInfo, error) {
	// Claude Code stores transcripts in ~/.claude/projects/PROJECT_NAME/
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	
	// Convert project path to Claude project directory name
	// Example: /home/user/dev/project -> -home-user-dev-project
	projectName := strings.ReplaceAll(projectPath, "/", "-")
	claudeProjectDir := filepath.Join(homeDir, ".claude", "projects", projectName)
	
	// Find all .jsonl files in the project directory
	pattern := filepath.Join(claudeProjectDir, "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	
	if len(matches) == 0 {
		return nil, nil // No transcript files found
	}
	
	// Find the most recently modified file
	var mostRecentFile string
	var mostRecentTime time.Time
	
	for _, file := range matches {
		info, err := os.Stat(file)
		if err != nil {
			continue // Skip files we can't stat
		}
		
		if info.ModTime().After(mostRecentTime) {
			mostRecentTime = info.ModTime()
			mostRecentFile = file
		}
	}
	
	if mostRecentFile == "" {
		return nil, nil
	}
	
	// Extract session ID from filename (UUID without .jsonl extension)
	filename := filepath.Base(mostRecentFile)
	sessionID := strings.TrimSuffix(filename, ".jsonl")
	
	return &TranscriptInfo{
		Path:      mostRecentFile,
		SessionID: sessionID,
	}, nil
}

// GetMostRecentActivity gets the most recent activity from the transcript, including system messages
func (tp *TranscriptParser) GetMostRecentActivity(transcriptPath string) (string, bool, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	// Read the last few lines to find the most recent activity
	lines, err := tp.readLinesFromEnd(file, 10)
	if err != nil {
		return "", false, err
	}

	// Process lines in reverse order (most recent first)
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // Skip malformed lines
		}

		// Extract content - check both message format and direct content format
		var content string
		
		// First check for direct content field (system entries)
		if directContent, ok := entry["content"].(string); ok {
			content = directContent
		} else if message, ok := entry["message"]; ok {
			// Check for message field (conversational entries)
			if msgMap, ok := message.(map[string]interface{}); ok {
				if role, ok := msgMap["role"].(string); ok {
					content = tp.extractMessageContent(msgMap, role)
				}
			}
		}

		if content != "" {
			isSystemOutput := tp.IsSystemOutput(content)
			return content, isSystemOutput, nil
		}
	}

	return "", false, nil
}

// GetLastMessageTimestampAndRole gets the timestamp and role of the most recent conversational message
func (tp *TranscriptParser) GetLastMessageTimestampAndRole(transcriptPath string) (time.Time, string, error) {
	lastEntry, err := tp.FindLastConversationalMessage(transcriptPath)
	if err != nil {
		return time.Time{}, "", err
	}
	
	if lastEntry == nil || lastEntry.Message == nil {
		return time.Time{}, "", nil
	}
	
	return lastEntry.Timestamp, lastEntry.Role, nil
}

// DetermineSessionStatus analyzes a transcript to determine if the session should be "waiting" or "running"
// Returns "waiting" if the last activity appears to be from a system/hook, "running" if conversational
func (tp *TranscriptParser) DetermineSessionStatus(transcriptPath string) string {
	// Get the most recent activity (including system messages)
	recentActivity, isSystemOutput, err := tp.GetMostRecentActivity(transcriptPath)
	if err != nil || recentActivity == "" {
		// If we can't determine activity, default to waiting
		return "waiting"
	}
	
	// If the most recent activity is system output, session should be waiting
	if isSystemOutput {
		return "waiting"
	}
	
	// If the most recent activity is conversational, session should be running
	return "running"
}