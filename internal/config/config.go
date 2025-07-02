package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const AppName = "coding-agent-dashboard"

// GetConfigDir returns the platform-appropriate config directory
func GetConfigDir() (string, error) {
	var baseDir string
	
	switch runtime.GOOS {
	case "windows":
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			baseDir = filepath.Join(homeDir, "AppData", "Roaming")
		}
	default: // Linux, macOS, etc.
		baseDir = os.Getenv("XDG_CONFIG_HOME")
		if baseDir == "" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			baseDir = filepath.Join(homeDir, ".config")
		}
	}
	
	configDir := filepath.Join(baseDir, AppName)
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", err
	}
	
	return configDir, nil
}