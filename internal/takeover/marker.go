package takeover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// MarkerFile is the file the service reads at start (kit-common
// config.TakeoverFile); the tool keeps it fresh while it runs.
const MarkerFile = ".devkit/takeover.json"

// MarkerFresh is the heartbeat interval; the service accepts a marker whose
// heartbeat is at most 15 seconds old.
const MarkerFresh = 5 * time.Second

// Marker mirrors kit-common's config.TakeoverMarker, key for key.
type Marker struct {
	Service   string            `json:"service"`
	Tag       string            `json:"tag"`
	Env       string            `json:"env"`
	PID       int               `json:"pid"`
	Heartbeat time.Time         `json:"heartbeat"`
	EnvVars   map[string]string `json:"env_vars"`
	Rewrite   map[string]string `json:"rewrite"`
	Services  map[string]string `json:"services"`
}

// WriteMarker writes the marker of serviceDir atomically with a fresh
// heartbeat.
func WriteMarker(serviceDir string, m Marker) error {
	m.Heartbeat = time.Now()
	m.PID = os.Getpid()
	path := filepath.Join(serviceDir, filepath.FromSlash(MarkerFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RemoveMarker deletes the marker; a missing one is fine.
func RemoveMarker(serviceDir string) {
	os.Remove(filepath.Join(serviceDir, filepath.FromSlash(MarkerFile)))
}
