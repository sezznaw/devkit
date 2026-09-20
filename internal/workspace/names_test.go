package workspace

import "testing"

func TestValidateServiceName(t *testing.T) {
	bad := []string{"func", "map", "type", "go", "string", "error", "new", "init", "main", "internal", "vendor", "handler", "idl", "common"}
	for _, n := range bad {
		if err := ValidateServiceName(n); err == nil {
			t.Errorf("%q must be rejected", n)
		}
	}
	// Verified to generate compiling services.
	good := []string{"user", "order-item", "log", "config", "context", "api", "client", "server", "service", "test", "kitex", "thrift", "cmd", "conf", "go-worker"}
	for _, n := range good {
		if err := ValidateServiceName(n); err != nil {
			t.Errorf("%q must be accepted: %v", n, err)
		}
	}
}
