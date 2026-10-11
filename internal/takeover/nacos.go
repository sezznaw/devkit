package takeover

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/clients/naming_client"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// Group and metadata keys shared with kit-common's devroute.
const (
	Group         = "DEV_TAKEOVER"
	BaselineGroup = "DEFAULT_GROUP"
	MetaTag       = "dev-tag"
	MetaOwner     = "dev-owner"
	MetaHost      = "dev-host"
	MetaSince     = "dev-since"
	MetaLocalPort = "dev-local-port"
)

// Nacos is the tool's own client, reached through the tunnel. Its ephemeral
// instances live as long as its connection: the tool dying takes them away.
type Nacos struct {
	naming    naming_client.INamingClient
	namespace string
}

// NewNacos connects to Nacos at localAddr ("127.0.0.1:18848"); the SDK finds
// gRPC at the port + 1000, which the tunnel layout guarantees.
func NewNacos(localAddr, namespace, username, password string) (*Nacos, error) {
	host, portStr, err := net.SplitHostPort(localAddr)
	if err != nil {
		return nil, fmt.Errorf("takeover: nacos address %q: %w", localAddr, err)
	}
	port, _ := strconv.ParseUint(portStr, 10, 16)
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("devkit-takeover-nacos-%d", os.Getpid()))
	cc := constant.NewClientConfig(
		constant.WithNamespaceId(namespace),
		constant.WithUsername(username),
		constant.WithPassword(password),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithUpdateCacheWhenEmpty(true),
		constant.WithDisableUseSnapShot(true),
		constant.WithLogDir(filepath.Join(dir, "log")),
		constant.WithCacheDir(filepath.Join(dir, "cache")),
		constant.WithLogLevel("error"),
	)
	naming, err := clients.NewNamingClient(vo.NacosClientParam{ClientConfig: cc, ServerConfigs: []constant.ServerConfig{*constant.NewServerConfig(host, port)}})
	if err != nil {
		return nil, fmt.Errorf("takeover: nacos client: %w", err)
	}
	return &Nacos{naming: naming, namespace: namespace}, nil
}

// Register announces the takeover instance.
func (n *Nacos) Register(service, ip string, port int, tag, localPort string) error {
	host, _ := os.Hostname()
	user := os.Getenv("USER")
	ok, err := n.naming.RegisterInstance(vo.RegisterInstanceParam{
		Ip: ip, Port: uint64(port), Weight: 10, Enable: true, Healthy: true, Ephemeral: true,
		ServiceName: service, GroupName: Group,
		Metadata: map[string]string{MetaTag: tag, MetaOwner: user, MetaHost: host, MetaSince: time.Now().UTC().Format(time.RFC3339), MetaLocalPort: localPort},
	})
	if err != nil {
		return fmt.Errorf("takeover: register in Nacos: %w", err)
	}
	if !ok {
		return fmt.Errorf("takeover: Nacos refused the registration")
	}
	return nil
}

// Deregister takes the instance away.
func (n *Nacos) Deregister(service, ip string, port int) error {
	_, err := n.naming.DeregisterInstance(vo.DeregisterInstanceParam{Ip: ip, Port: uint64(port), ServiceName: service, GroupName: Group, Ephemeral: true})
	return err
}

// Takeover is one instance of the takeover group.
type Takeover struct {
	Service, Tag, Owner, Host, Addr, Since, LocalPort string
}

// Takeovers lists the takeover group's instances of the services.
func (n *Nacos) Takeovers(services []string) ([]Takeover, error) {
	var out []Takeover
	for _, s := range services {
		list, err := n.naming.SelectAllInstances(vo.SelectAllInstancesParam{ServiceName: s, GroupName: Group})
		if err != nil {
			if isNoService(err) {
				continue
			}
			return nil, fmt.Errorf("takeover: list %s: %w", s, err)
		}
		for _, in := range list {
			if !in.Enable || !in.Healthy {
				continue
			}
			out = append(out, Takeover{Service: s, Tag: in.Metadata[MetaTag], Owner: in.Metadata[MetaOwner], Host: in.Metadata[MetaHost],
				Addr: net.JoinHostPort(in.Ip, strconv.FormatUint(in.Port, 10)), Since: in.Metadata[MetaSince], LocalPort: in.Metadata[MetaLocalPort]})
		}
	}
	return out, nil
}

// BaselineIPs lists the IPs of the baseline instances of a service: the
// "did my local service register itself" check.
func (n *Nacos) BaselineIPs(service string) ([]string, error) {
	list, err := n.naming.SelectAllInstances(vo.SelectAllInstancesParam{ServiceName: service, GroupName: BaselineGroup})
	if err != nil {
		if isNoService(err) {
			return nil, nil
		}
		return nil, err
	}
	var ips []string
	for _, in := range list {
		ips = append(ips, in.Ip)
	}
	return ips, nil
}

// Close closes the client; Nacos removes the ephemeral instances.
func (n *Nacos) Close() { n.naming.CloseClient() }

func isNoService(err error) bool {
	s := err.Error()
	return contains(s, "not found") || contains(s, "instance list is empty") || contains(s, "does not exist")
}

func contains(s, sub string) bool { return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// LocalIPs lists this machine's non-loopback IPs.
func LocalIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}
