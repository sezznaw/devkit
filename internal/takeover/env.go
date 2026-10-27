// Package takeover implements `devkit takeover`: a developer's machine takes
// one service of the development environment over for the requests that
// carry the developer's tag (kit-common's devroute does the routing). The
// tool opens one ssh connection to the environment's node, tunnels every
// cluster address the local service needs, registers the local instance in
// the Nacos group DEV_TAKEOVER through that tunnel while the local port is
// listening, and keeps a marker file the service reads at start. [takeover]
package takeover

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// Env is what the node side (`devkit takeover env`, run by sshd's
// ForceCommand) tells the tool about the environment. Project facts (which
// namespaces, which secrets) are flags of that command on the node, kept by
// the infrastructure repository; devkit itself knows none of them.
type Env struct {
	Version int    `json:"version"`
	NodeIP  string `json:"node_ip"`
	// Services are the project's services: one Kubernetes Service each.
	Services []Service `json:"services"`
	// Infra is every Service of the infrastructure namespaces (Nacos, MySQL,
	// Valkey, Redpanda, Centrifugo, S3, StarRocks, Tempo ...).
	Infra []Endpoint `json:"infra"`
	// EnvVars are the environment variables of the deployment of the service
	// asked for, secrets resolved: passwords, Nacos account, NACOS_ADDR,
	// NACOS_NAMESPACE, OTEL endpoint, TENANT_CODE.
	EnvVars map[string]string `json:"env_vars"`
	// ServiceEnvVars are, when every service was asked for (AllServices),
	// the environment variables of each backend's deployment the way EnvVars
	// is for one: what devkit tunnel prints per service. Keyed by deployment.
	ServiceEnvVars map[string]map[string]string `json:"service_env_vars,omitempty"`
	// ServiceToken is a token of the ServiceAccount of the service asked for,
	// signed for the audience its deployment's projected token has: what a Pod
	// of the service would hold, so the laptop's calls into the environment are
	// proven as that service. Short-lived; the tool renews it with every
	// refresh. Empty when the deployment projects none; ServiceTokenNote then
	// says why when one was expected.
	ServiceToken     string `json:"service_token,omitempty"`
	ServiceTokenNote string `json:"service_token_note,omitempty"`
}

// Service is a business service: Name, where its Service is and the port its
// container listens on (the local instance listens on the same one).
type Service struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	ClusterIP  string `json:"cluster_ip"`
	Port       int    `json:"port"`        // the Service port, 80
	TargetPort int    `json:"target_port"` // the container port, 8120
}

// Endpoint is an infrastructure Service: ClusterIP (empty for a headless
// Service) with its ports, and the Pods behind it (headless: the targets).
type Endpoint struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	ClusterIP string `json:"cluster_ip,omitempty"`
	Ports     []int  `json:"ports"`
	Pods      []Pod  `json:"pods,omitempty"`
}

// Pod is one Pod behind a headless Service: its name (the DNS label) and IP.
type Pod struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// CollectOptions are the node-side flags.
type CollectOptions struct {
	Kubeconfig string
	Namespaces []string // the services' namespaces (ser-vendor lives apart from the rest)
	Infra      []string // infrastructure namespaces
	Secrets    []string // "namespace/name" of the secrets a deployment may draw env vars from
	Service    string   // whose deployment's env to include; "" for none, AllServices for every backend
}

// AllServices asks the node side for every backend's environment variables
// (Env.ServiceEnvVars) instead of one service's.
const AllServices = "*"

// Collect runs on the node and gathers Env with kubectl.
func Collect(o CollectOptions) (*Env, error) {
	k := kubectl{kubeconfig: o.Kubeconfig, secretData: map[string]map[string]string{}}
	env := &Env{Version: 1, EnvVars: map[string]string{}}
	var nodes struct {
		Items []struct {
			Status struct {
				Addresses []struct{ Type, Address string } `json:"addresses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := k.get(&nodes, "nodes"); err != nil {
		return nil, err
	}
	for _, n := range nodes.Items {
		for _, a := range n.Status.Addresses {
			if a.Type == "InternalIP" && env.NodeIP == "" {
				env.NodeIP = a.Address
			}
		}
	}
	if env.NodeIP == "" {
		return nil, fmt.Errorf("takeover env: no node with an InternalIP")
	}
	for _, ns := range o.Namespaces {
		svcs, err := k.services(ns)
		if err != nil {
			return nil, err
		}
		for _, s := range svcs.Items {
			if len(s.Spec.Ports) == 0 || s.Spec.ClusterIP == "" || s.Spec.ClusterIP == "None" {
				continue
			}
			p := s.Spec.Ports[0]
			env.Services = append(env.Services, Service{Name: s.Metadata.Name, Namespace: ns, ClusterIP: s.Spec.ClusterIP, Port: p.Port, TargetPort: p.TargetPort.Int()})
		}
	}
	for _, ns := range o.Infra {
		list, err := k.services(ns)
		if err != nil {
			return nil, err
		}
		var eps struct {
			Items []struct {
				Metadata struct{ Name string } `json:"metadata"`
				Subsets  []struct {
					Addresses []struct {
						IP        string                 `json:"ip"`
						Hostname  string                 `json:"hostname"`
						TargetRef *struct{ Name string } `json:"targetRef"`
					} `json:"addresses"`
				} `json:"subsets"`
			} `json:"items"`
		}
		if err := k.get(&eps, "endpoints", "-n", ns); err != nil {
			return nil, err
		}
		pods := map[string][]Pod{}
		for _, e := range eps.Items {
			for _, ss := range e.Subsets {
				for _, a := range ss.Addresses {
					name := a.Hostname
					if name == "" && a.TargetRef != nil {
						name = a.TargetRef.Name
					}
					pods[e.Metadata.Name] = append(pods[e.Metadata.Name], Pod{Name: name, IP: a.IP})
				}
			}
		}
		for _, s := range list.Items {
			ep := Endpoint{Name: s.Metadata.Name, Namespace: ns, Pods: pods[s.Metadata.Name]}
			if s.Spec.ClusterIP != "" && s.Spec.ClusterIP != "None" {
				ep.ClusterIP = s.Spec.ClusterIP
			}
			for _, p := range s.Spec.Ports {
				ep.Ports = append(ep.Ports, p.Port)
			}
			if len(ep.Ports) == 0 || (ep.ClusterIP == "" && len(ep.Pods) == 0) {
				continue
			}
			env.Infra = append(env.Infra, ep)
		}
	}
	// The plain environment variables of a deployment (NACOS_ADDR,
	// NACOS_NAMESPACE, the OTLP endpoint, TENANT_CODE) describe the
	// environment, not one service. The service asked for comes first; when
	// none is named, or it has none, the first deployment that carries
	// NACOS_ADDR (a backend) provides them. The same deployment decides which
	// secrets are handed out: only those it draws env vars from (envFrom or a
	// secretKeyRef), and among those only the ones the node's --secret list
	// allows. A developer gets what the service would have in the cluster,
	// not every credential of the environment.
	var deps []deployment
	for _, ns := range o.Namespaces {
		var list struct {
			Items []deployment `json:"items"`
		}
		if err := k.get(&list, "deployments", "-n", ns); err != nil {
			return nil, err
		}
		for i := range list.Items {
			list.Items[i].Metadata.Namespace = ns
		}
		deps = append(deps, list.Items...)
	}
	var chosen *deployment
	for i := range deps {
		if deps[i].Metadata.Name == o.Service {
			chosen = &deps[i]
		}
	}
	if o.Service != "" && o.Service != AllServices && chosen == nil {
		return nil, fmt.Errorf("takeover env: no deployment %q in %s (is it a service of this environment?)", o.Service, strings.Join(o.Namespaces, ", "))
	}
	if chosen == nil || chosen.plain()["NACOS_ADDR"] == "" {
		for i := range deps {
			if deps[i].plain()["NACOS_ADDR"] != "" {
				chosen = &deps[i]
				break
			}
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("takeover env: no deployment with NACOS_ADDR in %s", strings.Join(o.Namespaces, ", "))
	}
	allowed := map[string]bool{}
	for _, ref := range o.Secrets {
		if _, _, ok := strings.Cut(ref, "/"); !ok {
			return nil, fmt.Errorf("takeover env: --secret %q must be namespace/name", ref)
		}
		allowed[ref] = true
	}
	vars, err := k.envOf(chosen, allowed)
	if err != nil {
		return nil, err
	}
	env.EnvVars = vars
	if o.Service == AllServices {
		env.ServiceEnvVars = map[string]map[string]string{}
		for i := range deps {
			if deps[i].plain()["NACOS_ADDR"] == "" {
				continue
			}
			vars, err := k.envOf(&deps[i], allowed)
			if err != nil {
				return nil, err
			}
			env.ServiceEnvVars[deps[i].Metadata.Name] = vars
		}
	}
	if chosen.Metadata.Name == o.Service {
		if src := chosen.projectedToken(); src != nil {
			tok, err := k.createToken(chosen.Metadata.Namespace, chosen.Spec.Template.Spec.ServiceAccountName, src.Audience, src.ExpirationSeconds)
			if err != nil {
				env.ServiceTokenNote = err.Error()
			} else {
				env.ServiceToken = tok
			}
		}
	}
	sort.Slice(env.Services, func(i, j int) bool { return env.Services[i].Name < env.Services[j].Name })
	sort.Slice(env.Infra, func(i, j int) bool {
		return env.Infra[i].Namespace+env.Infra[i].Name < env.Infra[j].Namespace+env.Infra[j].Name
	})
	return env, nil
}

type kubectl struct {
	kubeconfig string
	secretData map[string]map[string]string // "namespace/name" -> data, read once
}

// envOf is the deployment's environment variables, secrets resolved: only
// the Secrets it draws from that allowed ("namespace/name") lists.
func (k kubectl) envOf(d *deployment, allowed map[string]bool) (map[string]string, error) {
	out := map[string]string{}
	for name, keys := range d.secrets() {
		ref := d.Metadata.Namespace + "/" + name
		if !allowed[ref] {
			continue
		}
		data, ok := k.secretData[ref]
		if !ok {
			var sec struct {
				Data map[string]string `json:"data"`
			}
			if err := k.get(&sec, "secret", name, "-n", d.Metadata.Namespace); err != nil {
				return nil, err
			}
			data = sec.Data
			k.secretData[ref] = data
		}
		for key, b64 := range data {
			if keys != nil && !keys[key] {
				continue
			}
			v, err := decodeBase64(b64)
			if err != nil {
				return nil, fmt.Errorf("takeover env: secret %s key %s: %w", ref, key, err)
			}
			out[key] = v
		}
	}
	for k, v := range d.plain() {
		out[k] = v
	}
	return out, nil
}

type serviceList struct {
	Items []struct {
		Metadata struct{ Name string } `json:"metadata"`
		Spec     struct {
			ClusterIP string `json:"clusterIP"`
			Ports     []struct {
				Port       int      `json:"port"`
				TargetPort intOrStr `json:"targetPort"`
			} `json:"ports"`
		} `json:"spec"`
	} `json:"items"`
}

// deployment is the part of a Deployment that Collect reads.
type deployment struct {
	Metadata struct{ Name, Namespace string } `json:"metadata"`
	Spec     struct {
		Template struct {
			Spec struct {
				ServiceAccountName string `json:"serviceAccountName"`
				Volumes            []struct {
					Projected *struct {
						Sources []struct {
							ServiceAccountToken *tokenSource `json:"serviceAccountToken"`
						} `json:"sources"`
					} `json:"projected"`
				} `json:"volumes"`
				Containers []struct {
					Env []struct {
						Name      string `json:"name"`
						Value     string `json:"value"`
						ValueFrom *struct {
							SecretKeyRef *struct{ Name, Key string } `json:"secretKeyRef"`
						} `json:"valueFrom"`
					} `json:"env"`
					EnvFrom []struct {
						SecretRef *struct{ Name string } `json:"secretRef"`
					} `json:"envFrom"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

// tokenSource is a projected ServiceAccount token of a deployment.
type tokenSource struct {
	Audience          string `json:"audience"`
	ExpirationSeconds int    `json:"expirationSeconds"`
	Path              string `json:"path"`
}

// projectedToken is the deployment's projected ServiceAccount token with an
// audience of its own (the one its service attaches to calls), nil if none.
func (d *deployment) projectedToken() *tokenSource {
	for _, v := range d.Spec.Template.Spec.Volumes {
		if v.Projected == nil {
			continue
		}
		for _, src := range v.Projected.Sources {
			if t := src.ServiceAccountToken; t != nil && t.Audience != "" {
				return t
			}
		}
	}
	return nil
}

// plain is the literal environment variables of the deployment's containers.
func (d *deployment) plain() map[string]string {
	out := map[string]string{}
	for _, c := range d.Spec.Template.Spec.Containers {
		for _, e := range c.Env {
			if e.Value != "" {
				out[e.Name] = e.Value
			}
		}
	}
	return out
}

// secrets names the Secrets the deployment draws environment variables from,
// each with the keys it uses, or nil when it takes the whole Secret (envFrom).
func (d *deployment) secrets() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, c := range d.Spec.Template.Spec.Containers {
		for _, ef := range c.EnvFrom {
			if ef.SecretRef != nil {
				out[ef.SecretRef.Name] = nil
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
				continue
			}
			ref := e.ValueFrom.SecretKeyRef
			if keys, whole := out[ref.Name]; whole && keys == nil {
				continue
			}
			if out[ref.Name] == nil {
				out[ref.Name] = map[string]bool{}
			}
			out[ref.Name][ref.Key] = true
		}
	}
	return out
}

func (k kubectl) services(ns string) (*serviceList, error) {
	var list serviceList
	if err := k.get(&list, "services", "-n", ns); err != nil {
		return nil, err
	}
	return &list, nil
}

func (k kubectl) get(out any, args ...string) error {
	full := append([]string{"get", "-o", "json"}, args...)
	cmd := exec.Command("kubectl", full...)
	if k.kubeconfig != "" {
		cmd.Env = append(os.Environ(), "KUBECONFIG="+k.kubeconfig)
	}
	cmd.Stderr = nil // deprecation warnings are not data
	b, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("takeover env: kubectl %s: %w", strings.Join(args, " "), err)
	}
	return json.Unmarshal(b, out)
}

// createToken asks the API for a token of the ServiceAccount (TokenRequest):
// the node's account may do this for the services' accounts only.
func (k kubectl) createToken(ns, account, audience string, seconds int) (string, error) {
	if account == "" || account == "default" {
		return "", fmt.Errorf("the deployment projects a token but runs as the namespace's default ServiceAccount")
	}
	if seconds < 600 {
		seconds = 3600
	}
	cmd := exec.Command("kubectl", "create", "token", account, "-n", ns, "--audience", audience, "--duration", strconv.Itoa(seconds)+"s")
	if k.kubeconfig != "" {
		cmd.Env = append(os.Environ(), "KUBECONFIG="+k.kubeconfig)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("kubectl create token %s -n %s: %v: %s (the node's account needs create on serviceaccounts/token)", account, ns, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(b)), nil
}

// intOrStr is a Kubernetes IntOrString port.
type intOrStr struct {
	n int
	s string
}

func (v *intOrStr) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &v.s)
	}
	return json.Unmarshal(b, &v.n)
}

func (v intOrStr) Int() int {
	if v.n != 0 {
		return v.n
	}
	n, _ := strconv.Atoi(v.s)
	return n
}
