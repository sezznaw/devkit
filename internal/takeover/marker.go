package takeover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

// AckFile is written by the service (kit-common config.TakeoverAckFile) the
// moment it enters takeover mode. The tool registers the local instance only
// when a live process has written it: a service that listens but runs
// against its local middleware (started before the tool, APP_ENV set to
// something else, an old kit-common) must not receive the environment's
// requests and answer them from local data.
const AckFile = MarkerFile + ".ack"

// Ack mirrors kit-common's config.TakeoverAck.
type Ack struct {
	Service string    `json:"service"`
	Tag     string    `json:"tag"`
	PID     int       `json:"pid"`
	Since   time.Time `json:"since"`
}

// ReadAck returns the acknowledgement of serviceDir when it names this
// service and tag, its process is alive and it was written after since
// (the moment this tool wrote its marker); otherwise an error saying what is
// missing, for the user.
func ReadAck(serviceDir, service, tag string, since time.Time) (*Ack, error) {
	b, err := os.ReadFile(filepath.Join(serviceDir, filepath.FromSlash(AckFile)))
	if err != nil {
		return nil, fmt.Errorf("no %s: the service has not entered takeover mode", AckFile)
	}
	var a Ack
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("%s: %v", AckFile, err)
	}
	if a.Service != service || a.Tag != tag {
		return nil, fmt.Errorf("%s names %s/%s, not %s/%s", AckFile, a.Service, a.Tag, service, tag)
	}
	if a.Since.Before(since) {
		return nil, fmt.Errorf("%s is from a service started before this tool (%s)", AckFile, a.Since.Local().Format(time.TimeOnly))
	}
	if !processAlive(a.PID) {
		return nil, fmt.Errorf("%s is from a process (%d) that is gone", AckFile, a.PID)
	}
	return &a, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
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
	os.Remove(filepath.Join(serviceDir, filepath.FromSlash(AckFile)))
}
