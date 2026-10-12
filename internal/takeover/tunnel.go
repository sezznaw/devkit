package takeover

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Tunnel is the one ssh connection to the node: a control master with the
// local forwards (-L, cluster addresses) and one remote forward (-R, the
// node's port that reaches the local service). The system ssh is used so
// the developer's keys, agent and ~/.ssh/config apply.
type Tunnel struct {
	Target string // takeover@host
	sock   string
	cmd    *exec.Cmd
	stderr bytes.Buffer
	done   chan error
}

// Reverse is the -R: nodeIP:RemotePort -> 127.0.0.1:LocalPort. A zero
// RemotePort means no -R (devkit tunnel).
type Reverse struct {
	NodeIP     string
	RemotePort int
	LocalPort  int
}

// errRemotePortTaken is what ssh says when the -R port is in use on the node.
const errRemotePortTaken = "remote port forwarding failed"

// Open starts the connection and waits until it answers; it returns an error
// naming the ssh output when it does not. ok=false with a nil error means
// the remote port was taken (try another).
//
// target may be an alias of ~/.ssh/config, but one without LocalForward /
// RemoteForward lines of its own: ExitOnForwardFailure makes any forward
// that cannot be set up fatal, and ClearAllForwardings would drop ours too.
func Open(ctx context.Context, target, name string, forwards []Forward, rev Reverse) (t *Tunnel, taken bool, err error) {
	// One control socket per service, not per process: a tool that died
	// without cleaning up (kill -9) leaves its ssh behind, and the next run
	// for the same service finds and ends it here; one still alive is refused.
	if err := EndStale(target, name); err != nil {
		return nil, false, err
	}
	sock := sockPath(name)
	args := []string{"-N", "-M", "-S", sock,
		"-o", "ControlPersist=no", "-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=2",
		"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new"}
	for _, f := range forwards {
		if f.Target == "" {
			continue
		}
		args = append(args, "-L", fmt.Sprintf("127.0.0.1:%d:%s", f.LocalPort, f.Target))
	}
	if rev.RemotePort > 0 {
		args = append(args, "-R", fmt.Sprintf("%s:%d:127.0.0.1:%d", rev.NodeIP, rev.RemotePort, rev.LocalPort))
	}
	args = append(args, target)
	t = &Tunnel{Target: target, sock: sock, done: make(chan error, 1)}
	t.cmd = exec.Command("ssh", args...)
	t.cmd.Stderr = &t.stderr
	// Its own process group: a Ctrl+C in the terminal must reach devkit,
	// which deregisters and removes the marker, not ssh first.
	t.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := t.cmd.Start(); err != nil {
		return nil, false, fmt.Errorf("takeover: start ssh: %w", err)
	}
	go func() { t.done <- t.cmd.Wait() }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			t.Close()
			return nil, false, ctx.Err()
		case err := <-t.done:
			out := strings.TrimSpace(t.stderr.String())
			if strings.Contains(out, errRemotePortTaken) {
				return nil, true, nil
			}
			return nil, false, fmt.Errorf("takeover: ssh %s exited: %v\n%s", target, err, out)
		default:
		}
		if t.alive() {
			return t, false, nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Close()
	return nil, false, fmt.Errorf("takeover: ssh %s did not come up in 20s\n%s", target, strings.TrimSpace(t.stderr.String()))
}

func sockPath(name string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("devkit-takeover-%s.sock", name))
}

var masterPID = regexp.MustCompile(`pid=(\d+)`)

// EndStale ends the ssh a previous run for name left behind (kill -9 does
// not clean up). A master whose devkit is still alive is not stale: then
// another devkit is running for the same name and this one refuses. Run it
// before planning ports, so the old forwards do not count as taken.
func EndStale(target, name string) error {
	sock := sockPath(name)
	if _, err := os.Stat(sock); err != nil {
		return nil
	}
	out, err := exec.Command("ssh", "-S", sock, "-O", "check", target).CombinedOutput()
	if err == nil {
		if m := masterPID.FindStringSubmatch(string(out)); m != nil {
			if pid, _ := strconv.Atoi(m[1]); pid > 0 && parentAlive(pid) {
				return fmt.Errorf("takeover: another devkit %s is running on this machine (ssh pid %d); stop it first", name, pid)
			}
		}
		_ = exec.Command("ssh", "-S", sock, "-O", "exit", target).Run()
		time.Sleep(300 * time.Millisecond)
	}
	os.Remove(sock)
	return nil
}

// parentAlive says whether the parent of pid (the devkit that started this
// ssh) is still running; an orphaned ssh has init as parent.
func parentAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	ppid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if ppid <= 1 {
		return false
	}
	return syscall.Kill(ppid, 0) == nil
}

// alive asks the control master.
func (t *Tunnel) alive() bool {
	return exec.Command("ssh", "-S", t.sock, "-O", "check", t.Target).Run() == nil
}

// Done is closed with ssh's exit error when the connection ends.
func (t *Tunnel) Done() <-chan error { return t.done }

// Forward adds a -L while running.
func (t *Tunnel) Forward(f Forward) error {
	return t.control("forward", "-L", fmt.Sprintf("127.0.0.1:%d:%s", f.LocalPort, f.Target))
}

// Cancel removes a -L while running.
func (t *Tunnel) Cancel(f Forward) error {
	return t.control("cancel", "-L", fmt.Sprintf("127.0.0.1:%d:%s", f.LocalPort, f.Target))
}

func (t *Tunnel) control(op string, spec ...string) error {
	args := append([]string{"-S", t.sock, "-O", op}, spec...)
	args = append(args, t.Target)
	if out, err := exec.Command("ssh", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("ssh -O %s: %v %s", op, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Close ends the connection.
func (t *Tunnel) Close() {
	if t == nil || t.cmd == nil {
		return
	}
	_ = exec.Command("ssh", "-S", t.sock, "-O", "exit", t.Target).Run()
	select {
	case <-t.done:
	case <-time.After(2 * time.Second):
		_ = t.cmd.Process.Kill()
	}
	os.Remove(t.sock)
}

// FetchEnv runs the node side: `ssh target <service>`; the ForceCommand of
// the takeover account answers with Env and ignores anything else.
func FetchEnv(ctx context.Context, target, service string) (*Env, error) {
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new", "-o", "ClearAllForwardings=yes", target, service) // env fetch: no forwards wanted
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("takeover: ssh %s: %v\n%s\n(is your public key in the takeover account? ask the person who runs the environment)", target, err, strings.TrimSpace(stderr.String()))
	}
	var env Env
	if err := decodeJSON(out, &env); err != nil {
		return nil, fmt.Errorf("takeover: the node answered with something that is not the environment description (%d bytes, %w); run `ssh %s` by hand and look at its first line (not shown here: it may hold credentials)", len(out), err, target)
	}
	if env.Version != 1 {
		return nil, fmt.Errorf("takeover: node side speaks version %d, this devkit expects 1; update devkit on one side", env.Version)
	}
	for _, k := range []string{"NACOS_NAMESPACE", "NACOS_USERNAME", "NACOS_PASSWORD"} {
		if env.EnvVars[k] == "" {
			return nil, fmt.Errorf("takeover: the node side did not return %s (its --secret / deployments give it); check the ForceCommand flags and RBAC on the node", k)
		}
	}
	return &env, nil
}

// PortOf parses a service.addr ("8100", ":8100", "0.0.0.0:8100").
func PortOf(addr string) (int, error) {
	addr = strings.TrimSpace(addr)
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		addr = addr[i+1:]
	}
	n, err := strconv.Atoi(addr)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("takeover: cannot read a port from service.addr %q", addr)
	}
	return n, nil
}
