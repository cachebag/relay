package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// A task running relayd directly cannot redirect output through a shell.
func openSchedulerLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

// Login tasks inherit the user's environment, which may not include the key
// saved by `relay scheduler start --service --public`.
func schedulerAuthKey(configDir string) (string, error) {
	if key := os.Getenv("TS_AUTHKEY"); key != "" {
		return key, nil
	}
	if configDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("no user config directory for the saved Tailscale key: %w", err)
		}
		configDir = filepath.Join(dir, "relay")
	}
	return savedAuthKey(filepath.Join(configDir, "cli.json"))
}

func savedAuthKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read scheduler settings %s: %w", path, err)
	}
	var settings struct {
		Server struct {
			AuthKey string `json:"ts_authkey"`
		} `json:"server"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		// Do not include JSON parser details: the file contains credentials.
		return "", fmt.Errorf("invalid scheduler settings in %s", path)
	}
	return settings.Server.AuthKey, nil
}
