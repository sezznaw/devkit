package takeover

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Options of one takeover.
type Options struct {
	Service    string // service name (the deployment and Nacos name)
	ServiceDir string // where the marker goes
	LocalPort  int    // the local instance's listen port
	Tag        string
	SSH        string // takeover@host
	Force      bool   // take over although someone else holds the same tag
	Out        io.Writer
}

// Runner is the state machine: TunnelUp -> WaitLocal <-> Active, ending on
// SIGINT / SIGTERM. The local service starting and stopping needs no restart
// of the tool; the tunnel dropping is rebuilt with backoff.
type Runner struct {
	o      Options
	env    *Env
	layout *Layout
	tunnel *Tunnel
	nacos  *Nacos
	remote int // the node port of the reverse forward
	active bool
}

// Run blocks until the context ends or a signal arrives.
func Run(ctx context.Context, o Options) error {
	if !validTag(o.Tag) {
		return fmt.Errorf("takeover: tag %q must match ^[a-z0-9-]{1,32}$ (it defaults to your GitLab user name; pass --tag, or set tag: in ~/.devkit/takeover.yaml)", o.Tag)
	}
	r := &Runner{o: o}
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer r.cleanup()
	if err := r.up(ctx); err != nil {
		return err
	}
	return r.loop(ctx)
}

func (r *Runner) say(format string, a ...any) {
	fmt.Fprintf(r.o.Out, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
}

// up fetches the environment, plans the ports, opens the tunnel, connects
// to Nacos through it, checks for a conflict and writes the marker.
func (r *Runner) up(ctx context.Context) error {
	env, err := FetchEnv(ctx, r.o.SSH, r.o.Service)
	if err != nil {
		return err
	}
	r.env = env
	if !r.serviceKnown() {
		return fmt.Errorf("takeover: the environment has no service %q (known: %s)", r.o.Service, r.serviceNames())
	}
	if err := EndStale(r.o.SSH, r.o.Service); err != nil {
		return err
	}
	layout, err := Plan(env, PortTaken)
	if err != nil {
		return err
	}
	if layout.NacosAddr == "" {
		return fmt.Errorf("takeover: the environment has no Nacos (no Service with port 8848 in the infrastructure namespaces)")
	}
	r.layout = layout
	for attempt := 0; attempt < 10; attempt++ {
		r.remote = RemotePort(r.o.Service, r.o.Tag, attempt)
		t, taken, err := Open(ctx, r.o.SSH, r.o.Service, layout.Forwards, Reverse{NodeIP: env.NodeIP, RemotePort: r.remote, LocalPort: r.o.LocalPort})
		if err != nil {
			return err
		}
		if taken {
			continue
		}
		r.tunnel = t
		break
	}
	if r.tunnel == nil {
		return fmt.Errorf("takeover: ten node ports in a row were taken; try again later")
	}
	var whats []string
	for _, f := range layout.Forwards {
		if KnownPort(f) {
			whats = append(whats, fmt.Sprintf("%s→%d", f.What, f.LocalPort))
		}
	}
	r.say("tunnel up: %d forwards (%s, …)", len(layout.Forwards), strings.Join(whats, ", "))
	r.say("advertise %s:%d → 127.0.0.1:%d", env.NodeIP, r.remote, r.o.LocalPort)

	if err := r.connectNacos(ctx); err != nil {
		return err
	}
	others, err := r.nacos.Takeovers(r.serviceNamesList())
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	for _, t := range others {
		if t.Service == r.o.Service && t.Tag == r.o.Tag && t.Host != host {
			if !r.o.Force {
				return fmt.Errorf("takeover: %s is already taken over with tag %q by %s on %s since %s; use another --tag, or --force to take it anyway", r.o.Service, t.Tag, t.Owner, t.Host, t.Since)
			}
			r.say("WARNING: replacing %s's takeover of %s (tag %s)", t.Owner, r.o.Service, t.Tag)
		}
	}
	r.mergeLocalTakeovers(others)
	if err := r.writeMarker(); err != nil {
		return err
	}
	r.say("waiting for the local service on :%d  (start it in your IDE as usual; the run configuration needs no change)", r.o.LocalPort)
	return nil
}

func (r *Runner) connectNacos(ctx context.Context) error {
	ns := r.env.EnvVars["NACOS_NAMESPACE"]
	user, pass := r.env.EnvVars["NACOS_USERNAME"], r.env.EnvVars["NACOS_PASSWORD"]
	var last error
	for i := 0; i < 10; i++ {
		n, err := NewNacos(r.layout.NacosAddr, ns, user, pass)
		if err == nil {
			r.nacos = n
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("takeover: cannot reach Nacos through the tunnel at %s: %w", r.layout.NacosAddr, last)
}

func (r *Runner) writeMarker() error {
	envVars := map[string]string{}
	for k, v := range r.layout.EnvVars {
		if k == "APP_ENV" {
			continue // the service stays "local, in takeover mode"
		}
		envVars[k] = v
	}
	profile := r.env.EnvVars["APP_ENV"]
	if profile == "" {
		profile = "dev"
	}
	return WriteMarker(r.o.ServiceDir, Marker{Service: r.o.Service, Tag: r.o.Tag, Env: profile, EnvVars: envVars, Rewrite: r.layout.Rewrite, Services: r.layout.Services})
}

// loop is WaitLocal <-> Active with the periodic chores.
func (r *Runner) loop(ctx context.Context) error {
	probe := time.NewTicker(time.Second)
	heartbeat := time.NewTicker(MarkerFresh)
	guard := time.NewTicker(10 * time.Second)
	refresh := time.NewTicker(60 * time.Second)
	defer probe.Stop()
	defer heartbeat.Stop()
	defer guard.Stop()
	defer refresh.Stop()
	up, down := 0, 0
	for {
		select {
		case <-ctx.Done():
			r.say("stopping")
			return nil
		case err := <-r.tunnel.Done():
			if ctx.Err() != nil { // Ctrl+C reached ssh first; cleanup follows
				r.say("stopping")
				return nil
			}
			r.say("tunnel dropped (%v); deregistering and reconnecting", err)
			r.deregister()
			up = 0 // register again once the tunnel is back and the port still listens
			if err := r.reconnect(ctx); err != nil {
				return err
			}
		case <-probe.C:
			if Listening(r.o.LocalPort) {
				up, down = up+1, 0
				if up >= 2 && !r.active && r.nacos != nil {
					r.register() // first time, after a tunnel drop, or after a failed attempt
				}
			} else {
				down, up = down+1, 0
				if down == 2 && r.active {
					r.deregister()
					r.say("local service stopped → deregistered, traffic back to %s's own instances", r.o.Service)
				}
			}
		case <-heartbeat.C:
			if err := r.writeMarker(); err != nil {
				r.say("WARNING: cannot refresh %s: %v", MarkerFile, err)
			}
		case <-guard.C:
			r.checkBaseline()
		case <-refresh.C:
			r.refresh(ctx)
		}
	}
}

func (r *Runner) register() {
	if err := r.nacos.Register(r.o.Service, r.env.NodeIP, r.remote, r.o.Tag, strconv.Itoa(r.o.LocalPort)); err != nil {
		r.say("WARNING: %v (will retry)", err)
		return
	}
	r.active = true
	r.say("local service is up → registered as takeover instance of %s", r.o.Service)
	r.say("send requests with the header  %s: %s", "X-Dev-Tag", r.o.Tag)
}

func (r *Runner) deregister() {
	if !r.active || r.nacos == nil {
		return
	}
	if err := r.nacos.Deregister(r.o.Service, r.env.NodeIP, r.remote); err != nil {
		r.say("WARNING: deregister: %v", err)
	}
	r.active = false
}

// checkBaseline warns when the local service announced itself as a normal
// instance (it bypassed takeover mode): everybody's requests would reach it.
func (r *Runner) checkBaseline() {
	ips, err := r.nacos.BaselineIPs(r.o.Service)
	if err != nil {
		return
	}
	mine := map[string]bool{}
	for _, ip := range LocalIPs() {
		mine[ip] = true
	}
	for _, ip := range ips {
		if mine[ip] {
			r.say("ERROR: an instance of %s at %s (this machine) is registered in %s: the local service registered itself as a normal instance and receives everybody's requests. Stop it; it must be started with the marker present (takeover mode), which does not register.", r.o.Service, ip, BaselineGroup)
		}
	}
}

// refresh re-reads the environment: headless Pods that moved get their
// forward re-pointed; services this machine took over as well are reached
// directly; the marker follows.
func (r *Runner) refresh(ctx context.Context) {
	env, err := FetchEnv(ctx, r.o.SSH, r.o.Service)
	if err != nil {
		r.say("WARNING: refresh: %v", err)
		return
	}
	// Our own ports count as free, everything else as it is: the plan then
	// reproduces our allocation and only the targets can differ.
	ours := map[int]bool{}
	old := map[string]Forward{}
	for _, f := range r.layout.Forwards {
		ours[f.LocalPort] = true
		old[f.What] = f
	}
	layout, err := Plan(env, func(p int) bool { return !ours[p] && PortTaken(p) })
	if err != nil {
		r.say("WARNING: refresh: %v", err)
		return
	}
	for i, f := range layout.Forwards {
		prev, ok := old[f.What]
		if !ok || f.Target == "" {
			continue
		}
		if prev.LocalPort != f.LocalPort {
			// the set of forwards changed shape; keep what the tunnel has
			layout.Forwards[i] = prev
			continue
		}
		if prev.Target != f.Target {
			_ = r.tunnel.Cancel(prev)
			if err := r.tunnel.Forward(f); err != nil {
				r.say("WARNING: re-point %s: %v", f.What, err)
				layout.Forwards[i] = prev
			} else {
				r.say("%s moved to %s; tunnel re-pointed", f.What, f.Target)
			}
		}
	}
	r.layout = layout
	r.env = env
	if list, err := r.nacos.Takeovers(r.serviceNamesList()); err == nil {
		r.mergeLocalTakeovers(list)
	}
	if err := r.writeMarker(); err != nil {
		r.say("WARNING: cannot refresh %s: %v", MarkerFile, err)
	}
}

// reconnect rebuilds the tunnel with backoff after it dropped.
func (r *Runner) reconnect(ctx context.Context) error {
	r.tunnel.Close()
	if r.nacos != nil {
		r.nacos.Close()
		r.nacos = nil
	}
	wait := time.Second
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		t, taken, err := Open(ctx, r.o.SSH, r.o.Service, r.layout.Forwards, Reverse{NodeIP: r.env.NodeIP, RemotePort: r.remote, LocalPort: r.o.LocalPort})
		if taken {
			r.say("reconnect: node port %d is taken now; retrying", r.remote)
		}
		if err == nil && !taken {
			r.tunnel = t
			if err := r.connectNacos(ctx); err != nil {
				r.say("WARNING: %v", err)
				t.Close()
			} else {
				r.say("tunnel back")
				return nil
			}
		} else if err != nil {
			r.say("reconnect failed: %v", err)
		}
		if wait < 30*time.Second {
			wait *= 2
		}
	}
}

func (r *Runner) cleanup() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		RemoveMarker(r.o.ServiceDir) // first: it holds credentials and must not outlive us
		r.deregister()
		if r.nacos != nil {
			r.nacos.Close()
		}
		r.tunnel.Close()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	r.say("done; traffic is back on %s's own instances", r.o.Service)
}

// mergeLocalTakeovers points the services this machine took over as well
// at their local instances, so two local services call each other directly.
func (r *Runner) mergeLocalTakeovers(list []Takeover) {
	host, _ := os.Hostname()
	for _, t := range list {
		if t.Host == host && t.Service != r.o.Service && t.LocalPort != "" {
			r.layout.Services[t.Service] = "127.0.0.1:" + t.LocalPort
		}
	}
}

func (r *Runner) serviceKnown() bool {
	for _, s := range r.env.Services {
		if s.Name == r.o.Service {
			return true
		}
	}
	return false
}

func (r *Runner) serviceNamesList() []string {
	var out []string
	for _, s := range r.env.Services {
		out = append(out, s.Name)
	}
	return out
}

func (r *Runner) serviceNames() string { return strings.Join(r.serviceNamesList(), ", ") }

func validTag(tag string) bool {
	if len(tag) == 0 || len(tag) > 32 {
		return false
	}
	for _, c := range tag {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// ErrNotListening etc. reserved for tests.
var errStopped = errors.New("stopped")
