package workspace

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// PortsFile is the project's port table, kept in the IDL repository because
// every developer has that checkout and every service's interface is
// decided there: one line per service, the first port of its block.
const PortsFile = "ports.yaml"

// The port plan (owner's rule, 2026-10-07): the gateway is 8080; every
// other service owns a block of ten from 8100 on (8100-8109, 8110-8119,
// ...): +0 is the RPC or HTTP port, +1 the callback listener, the rest is
// reserved for what comes later. Ports 9000-9999 stay free for port-forwards
// to dev (port + 1000) and 10000+ for operations tools.
const (
	GatewayPort    = 8080
	PortBlockStart = 8100
	PortBlockSize  = 10
	PortBlockEnd   = 8990
)

// Ports is the table: service -> first port of its block.
type Ports map[string]int

// LoadPorts reads <idlDir>/ports.yaml; a missing file is an empty table.
func LoadPorts(idlDir string) (Ports, error) {
	f, err := os.Open(filepath.Join(idlDir, PortsFile))
	if os.IsNotExist(err) {
		return Ports{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p := Ports{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || line == "ports:" {
			continue
		}
		name, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(val, "#", 2)[0]))
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a port", PortsFile, line)
		}
		p[strings.TrimSpace(name)] = n
	}
	return p, sc.Err()
}

// AllocatePort returns the port of service, assigning the next free block
// (or GatewayPort to the first API service) when the table has none, and
// writes the table back. assigned says whether a new entry was made: the
// caller commits ports.yaml together with the service's IDL.
func AllocatePort(idlDir, service string, api bool) (port int, assigned bool, err error) {
	ports, err := LoadPorts(idlDir)
	if err != nil {
		return 0, false, err
	}
	if p, ok := ports[service]; ok {
		return p, false, nil
	}
	used := map[int]bool{}
	for _, p := range ports {
		used[p] = true
	}
	switch {
	case api && !used[GatewayPort]:
		port = GatewayPort
	default:
		for port = PortBlockStart; port <= PortBlockEnd; port += PortBlockSize {
			if !used[port] {
				break
			}
		}
		if port > PortBlockEnd {
			return 0, false, fmt.Errorf("%s: all port blocks from %d to %d are taken", PortsFile, PortBlockStart, PortBlockEnd)
		}
	}
	ports[service] = port
	if err := savePorts(idlDir, ports); err != nil {
		return 0, false, err
	}
	return port, true, nil
}

func savePorts(idlDir string, ports Ports) error {
	names := make([]string, 0, len(ports))
	for n := range ports {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		return ports[names[i]] < ports[names[j]] || ports[names[i]] == ports[names[j]] && names[i] < names[j]
	})
	var b strings.Builder
	b.WriteString(`# Port table of the project, written by devkit ngs / nas. One line per service: the
# first port of its block. The gateway is 8080; every other service owns ten ports
# from 8100 on: +0 RPC/HTTP, +1 callback listener, +2..+9 reserved. The same port
# in every environment; the deployment's values follow it. Commit this file with
# the service's IDL. 9000-9999 are port-forwards to dev (+1000), 10000+ operations.
#
# 项目端口表，由 devkit ngs / nas 写入。一个服务一行：它那一段的首端口。网关 8080；
# 其他服务从 8100 起每个占十个端口：+0 RPC/HTTP，+1 回调监听，+2..+9 预留。四个环境
# 用同一个端口，部署的 values 跟它走。随服务的 IDL 一起提交。
# 本机 9000-9999 是转发到 dev 的端口（+1000），10000 以上是运维工具。
ports:
`)
	for _, n := range names {
		fmt.Fprintf(&b, "  %s: %d\n", n, ports[n])
	}
	if err := os.MkdirAll(idlDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(idlDir, PortsFile), []byte(b.String()), 0o644)
}
