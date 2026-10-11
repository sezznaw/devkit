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
	Namespace  string   // the services' namespace
	Infra      []string // infrastructure namespaces
	Secrets    []string // "namespace/name" of the secrets whose keys become env vars
	Service    string   // whose deployment's env to include; "" for none
}

// Collect runs on the node and gathers Env with kubectl.
func Collect(o CollectOptions) (*Env, error) {
	k := kubectl{kubeconfig: o.Kubeconfig}
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
	svcs, err := k.services(o.Namespace)
	if err != nil {
		return nil, err
	}
	for _, s := range svcs.Items {
		if len(s.Spec.Ports) == 0 || s.Spec.ClusterIP == "" || s.Spec.ClusterIP == "None" {
			continue
		}
		p := s.Spec.Ports[0]
		env.Services = append(env.Services, Service{Name: s.Metadata.Name, Namespace: o.Namespace, ClusterIP: s.Spec.ClusterIP, Port: p.Port, TargetPort: p.TargetPort.Int()})
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
	for _, ref := range o.Secrets {
		ns, name, ok := strings.Cut(ref, "/")
		if !ok {
			return nil, fmt.Errorf("takeover env: --secret %q must be namespace/name", ref)
		}
		var sec struct {
			Data map[string]string `json:"data"`
		}
		if err := k.get(&sec, "secret", name, "-n", ns); err != nil {
			return nil, err
		}
		for key, b64 := range sec.Data {
			v, err := decodeBase64(b64)
			if err != nil {
				return nil, fmt.Errorf("takeover env: secret %s key %s: %w", ref, key, err)
			}
			env.EnvVars[key] = v
		}
	}
	// The plain environment variables of a deployment (NACOS_ADDR,
	// NACOS_NAMESPACE, the OTLP endpoint, TENANT_CODE) describe the
	// environment, not one service. The service asked for comes first; when
	// none is named, or it has none, the first deployment that carries
	// NACOS_ADDR (a backend) provides them.
	var deps struct {
		Items []struct {
			Metadata struct{ Name string } `json:"metadata"`
			Spec     struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Env []struct {
								Name  string `json:"name"`
								Value string `json:"value"`
							} `json:"env"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := k.get(&deps, "deployments", "-n", o.Namespace); err != nil {
		return nil, err
	}
	plain := func(name string) map[string]string {
		for _, d := range deps.Items {
			if d.Metadata.Name != name {
				continue
			}
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
		return nil
	}
	chosen := plain(o.Service)
	if chosen["NACOS_ADDR"] == "" {
		for _, d := range deps.Items {
			if p := plain(d.Metadata.Name); p["NACOS_ADDR"] != "" {
				chosen = p
				break
			}
		}
	}
	if o.Service != "" && plain(o.Service) == nil {
		return nil, fmt.Errorf("takeover env: no deployment %q in %s (is it a service of this environment?)", o.Service, o.Namespace)
	}
	for k, v := range chosen {
		env.EnvVars[k] = v
	}
	sort.Slice(env.Services, func(i, j int) bool { return env.Services[i].Name < env.Services[j].Name })
	sort.Slice(env.Infra, func(i, j int) bool {
		return env.Infra[i].Namespace+env.Infra[i].Name < env.Infra[j].Namespace+env.Infra[j].Name
	})
	return env, nil
}

type kubectl struct{ kubeconfig string }

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
