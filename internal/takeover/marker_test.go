package takeover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ReadAck accepts only a live, matching acknowledgement written after the
// tool started; everything else says why.
func TestReadAck(t *testing.T) {
	dir := t.TempDir()
	write := func(a Ack) {
		b, _ := json.Marshal(a)
		if err := os.MkdirAll(filepath.Join(dir, ".devkit"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(AckFile)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now().Add(-time.Minute)
	if _, err := ReadAck(dir, "ser-member", "zoe", started); err == nil {
		t.Fatal("no file accepted")
	}
	write(Ack{Service: "ser-member", Tag: "zoe", PID: os.Getpid(), Since: time.Now()})
	if _, err := ReadAck(dir, "ser-member", "zoe", started); err != nil {
		t.Fatalf("good ack refused: %v", err)
	}
	write(Ack{Service: "ser-member", Tag: "bob", PID: os.Getpid(), Since: time.Now()})
	if _, err := ReadAck(dir, "ser-member", "zoe", started); err == nil {
		t.Fatal("other tag accepted")
	}
	write(Ack{Service: "ser-member", Tag: "zoe", PID: os.Getpid(), Since: started.Add(-time.Hour)})
	if _, err := ReadAck(dir, "ser-member", "zoe", started); err == nil {
		t.Fatal("ack older than the tool accepted")
	}
	write(Ack{Service: "ser-member", Tag: "zoe", PID: 2147483000, Since: time.Now()})
	if _, err := ReadAck(dir, "ser-member", "zoe", started); err == nil {
		t.Fatal("dead process accepted")
	}
	RemoveMarker(dir)
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(AckFile))); err == nil {
		t.Fatal("RemoveMarker left the ack")
	}
}
