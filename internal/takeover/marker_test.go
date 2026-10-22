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

// The token sits beside the marker, owner-only; "" and RemoveMarker take it away.
func TestWriteToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".devkit"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(TokenFile))
	if err := WriteToken(dir, "tok"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "tok\n" {
		t.Fatalf("token file %q", b)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if err := WriteToken(dir, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("empty token left the file")
	}
	_ = WriteToken(dir, "tok")
	RemoveMarker(dir)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("RemoveMarker left the token")
	}
}

// The projected token of a deployment is found among its volumes; a plain
// projected volume (configMap, the API token without an audience) is not one.
func TestProjectedToken(t *testing.T) {
	var d deployment
	spec := `{"metadata":{"name":"ser-member"},"spec":{"template":{"spec":{"serviceAccountName":"ser-member","volumes":[
		{"name":"cfg","projected":{"sources":[{"configMap":{"name":"x"}},{"serviceAccountToken":{"path":"api-token"}}]}},
		{"name":"sportsbook-identity","projected":{"sources":[{"serviceAccountToken":{"audience":"sportsbook-rpc","expirationSeconds":3600,"path":"rpc-token"}}]}}]}}}}`
	if err := json.Unmarshal([]byte(spec), &d); err != nil {
		t.Fatal(err)
	}
	src := d.projectedToken()
	if src == nil || src.Audience != "sportsbook-rpc" || src.ExpirationSeconds != 3600 || d.Spec.Template.Spec.ServiceAccountName != "ser-member" {
		t.Fatalf("%+v", src)
	}
	var none deployment
	_ = json.Unmarshal([]byte(`{"spec":{"template":{"spec":{"volumes":[{"name":"v","emptyDir":{}}]}}}}`), &none)
	if none.projectedToken() != nil {
		t.Fatal("found a token where there is none")
	}
}
