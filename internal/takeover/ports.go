package takeover

import (
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"net"
	"sort"
	"strconv"
	"strings"
)

// Layout is what the tool makes of Env on the developer's machine: which
// local port tunnels to which cluster address, the rewrite table the service
// gets, and the environment variables with cluster addresses replaced.
//
// Ports follow the team's rule for reaching the environment from a laptop:
// a business service is its container port plus 1000 (ser-auth 8120 ->
// 9120), infrastructure is the port plus 10000 (MySQL 3306 -> 13306, Nacos
// 8848 -> 18848 and 9848 -> 19848). A port already taken moves up to the
// next free one; the rewrite table is what the service follows, the rule
// only makes the numbers predictable.
type Layout struct {
	Forwards []Forward
	// Rewrite maps "host:port" as the cluster names it (every DNS form,
	// ClusterIP, Pod IP) to "127.0.0.1:port".
	Rewrite map[string]string
	// Services maps a service name to "127.0.0.1:port".
	Services map[string]string
	// EnvVars are Env.EnvVars with cluster addresses rewritten.
	EnvVars map[string]string
	// ServiceEnvVars are Env.ServiceEnvVars, rewritten the same way.
	ServiceEnvVars map[string]map[string]string
	// NacosAddr is the local Nacos address ("127.0.0.1:18848"), "" when the
	// environment has no Service with port 8848.
	NacosAddr string
}

// Forward is one -L: local port to a cluster "ip:port".
type Forward struct {
	LocalPort int
	Target    string // ip:port
	What      string // for the terminal: "nacos 8848", "ser-auth"
}

// nacosPort and the gRPC port offset the Nacos SDK derives.
const (
	nacosPort       = 8848
	nacosGRPCOffset = 1000
)

// Plan computes the Layout. taken says whether a local port is in use.
func Plan(env *Env, taken func(port int) bool) (*Layout, error) {
	return PlanWith(env, taken, nil)
}

// PlanWith is Plan with the ports of a previous layout to keep: prefer maps
// a forward's What to its local port. A refresh plans with the live layout's
// ports so that the rewrite table and the tunnel never disagree, whatever
// changed in the environment (a Pod gone, a Service added); only new names
// get new ports.
func PlanWith(env *Env, taken func(port int) bool, prefer map[string]int) (*Layout, error) {
	l := &Layout{Rewrite: map[string]string{}, Services: map[string]string{}, EnvVars: map[string]string{}}
	used := map[int]bool{}
	for _, p := range prefer {
		used[p] = true // reserved for their names below
	}
	pick := func(what string, want int) int {
		if p, ok := prefer[what]; ok {
			return p
		}
		for p := want; p < 65535; p++ {
			if !used[p] && !taken(p) {
				used[p] = true
				return p
			}
		}
		return 0
	}
	// Nacos first: its two ports must stay 1000 apart.
	for _, ep := range env.Infra {
		if !hasPort(ep.Ports, nacosPort) {
			continue
		}
		// Both ports of a pair must be free (a second tool on this machine
		// holds the first pair).
		main := 0
		if p, ok := prefer[fmt.Sprintf("%s %d", ep.Name, nacosPort)]; ok {
			main = p
		}
		for p := nacosPort + 10000; main == 0 && p < nacosPort+10000+500; p++ {
			if !used[p] && !taken(p) && !used[p+nacosGRPCOffset] && !taken(p+nacosGRPCOffset) {
				main = p
			}
		}
		if main == 0 {
			return nil, fmt.Errorf("takeover: no free pair of local ports for Nacos (main and main+1000, from %d up)", nacosPort+10000)
		}
		grpc := nacosPort + nacosGRPCOffset
		used[main], used[main+nacosGRPCOffset] = true, true
		l.addInfra(ep, nacosPort, main)
		l.addInfra(ep, grpc, main+nacosGRPCOffset)
		l.NacosAddr = fmt.Sprintf("127.0.0.1:%d", main)
	}
	// Services with a ClusterIP get the rule's port first; a headless twin
	// on the same port (StarRocks' -search next to -service) moves up.
	infra := append([]Endpoint(nil), env.Infra...)
	sort.SliceStable(infra, func(i, j int) bool { return infra[i].ClusterIP != "" && infra[j].ClusterIP == "" })
	for _, ep := range infra {
		seen := map[int]bool{} // a Service lists 9094/TCP and 9094/UDP as two ports; one forward
		for _, p := range ep.Ports {
			if seen[p] || hasPort(ep.Ports, nacosPort) && (p == nacosPort || p == nacosPort+nacosGRPCOffset) {
				continue
			}
			seen[p] = true
			local := pick(fmt.Sprintf("%s %d", ep.Name, p), infraLocalPort(p))
			if local == 0 {
				return nil, fmt.Errorf("takeover: no free local port for %s:%d", ep.Name, p)
			}
			l.addInfra(ep, p, local)
		}
	}
	for _, s := range env.Services {
		want := s.TargetPort + 1000
		if s.TargetPort == 0 {
			want = s.Port + 1000
		}
		local := pick(s.Name, want)
		if local == 0 {
			return nil, fmt.Errorf("takeover: no free local port for service %s", s.Name)
		}
		target := net.JoinHostPort(s.ClusterIP, strconv.Itoa(s.Port))
		l.Forwards = append(l.Forwards, Forward{LocalPort: local, Target: target, What: s.Name})
		to := fmt.Sprintf("127.0.0.1:%d", local)
		l.Services[s.Name] = to
		for _, h := range dnsNames(s.Name, s.Namespace) {
			l.Rewrite[h+":"+strconv.Itoa(s.Port)] = to
		}
		l.Rewrite[target] = to
	}
	for k, v := range env.EnvVars {
		l.EnvVars[k] = l.rewriteValue(v)
	}
	if len(env.ServiceEnvVars) > 0 {
		l.ServiceEnvVars = map[string]map[string]string{}
		for name, vars := range env.ServiceEnvVars {
			m := map[string]string{}
			for k, v := range vars {
				m[k] = l.rewriteValue(v)
			}
			l.ServiceEnvVars[name] = m
		}
	}
	sort.Slice(l.Forwards, func(i, j int) bool { return l.Forwards[i].LocalPort < l.Forwards[j].LocalPort })
	return l, nil
}

func (l *Layout) addInfra(ep Endpoint, port, local int) {
	to := fmt.Sprintf("127.0.0.1:%d", local)
	ps := strconv.Itoa(port)
	target := ""
	if ep.ClusterIP != "" {
		target = net.JoinHostPort(ep.ClusterIP, ps)
		l.Rewrite[target] = to
	} else if len(ep.Pods) > 0 {
		target = net.JoinHostPort(ep.Pods[0].IP, ps)
	}
	for _, h := range dnsNames(ep.Name, ep.Namespace) {
		l.Rewrite[h+":"+ps] = to
	}
	for _, p := range ep.Pods {
		l.Rewrite[net.JoinHostPort(p.IP, ps)] = to
		if p.Name != "" {
			for _, h := range dnsNames(p.Name+"."+ep.Name, ep.Namespace) {
				l.Rewrite[h+":"+ps] = to
			}
		}
	}
	l.Forwards = append(l.Forwards, Forward{LocalPort: local, Target: target, What: fmt.Sprintf("%s %d", ep.Name, port)})
}

// dropPort removes every rewrite entry that points at a local port whose
// forward could not be opened: better an unrewritten cluster address that
// fails visibly than one that points at a port nobody listens on.
func (l *Layout) dropPort(local int) {
	to := fmt.Sprintf("127.0.0.1:%d", local)
	for k, v := range l.Rewrite {
		if v == to {
			delete(l.Rewrite, k)
		}
	}
	for k, v := range l.Services {
		if v == to {
			delete(l.Services, k)
		}
	}
}

func dnsNames(name, ns string) []string {
	return []string{name + "." + ns, name + "." + ns + ".svc", name + "." + ns + ".svc.cluster.local"}
}

// rewriteValue replaces every "host:port" of the table inside a value
// (NACOS_ADDR, an OTLP endpoint, a URL); longest keys first so a name does
// not shadow its longer forms.
func (l *Layout) rewriteValue(v string) string {
	keys := make([]string, 0, len(l.Rewrite))
	for k := range l.Rewrite {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		v = strings.ReplaceAll(v, k, l.Rewrite[k])
	}
	return v
}

// infraLocalPort is the rule's port + 10000, or, when that leaves the port
// range (OTLP's 55680 and friends), a slot in 30000-39999.
func infraLocalPort(p int) int {
	if p+10000 <= 65535 {
		return p + 10000
	}
	return 30000 + p%10000
}

func hasPort(ports []int, p int) bool {
	for _, x := range ports {
		if x == p {
			return true
		}
	}
	return false
}

// RemotePort is the port on the node the takeover instance is reached at:
// 20000 + hash(service, tag) % 10000, plus attempt when the first is taken.
func RemotePort(service, tag string, attempt int) int {
	h := fnv.New32a()
	h.Write([]byte(service + "/" + tag))
	return 20000 + int(h.Sum32()%10000) + attempt
}

// PortTaken says whether a local TCP port cannot be bound.
func PortTaken(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	l.Close()
	return false
}

func decodeBase64(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

// KnownPort says whether a forward is one a developer would connect to by
// hand: the main ports of MySQL, Redis, Nacos, Kafka, StarRocks, S3. Used to
// keep terminal output short.
func KnownPort(f Forward) bool {
	i := strings.LastIndex(f.Target, ":")
	if i < 0 {
		return false
	}
	name := strings.ToLower(f.What)
	switch f.Target[i+1:] {
	case "3306", "6379", "8848", "9030", "8333":
		return true
	case "9093", "9092":
		return strings.Contains(name, "redpanda") || strings.Contains(name, "kafka")
	}
	return false
}
