package lint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IDLDepsFile is written by `make gen` next to idl.lock: the IDL generation
// of the service and of each RPC service it calls (how many IDL commits
// touched that service's directory up to the locked commit). The binary
// embeds a copy; instances register their generation in Nacos and a service
// waits at start until the ones it calls are at least as new (the dependency
// gate in kit-common), so releases need no order. Computed from the IDL
// history, which CI's shallow clone does not have, so it is committed with
// the service, like idl.lock.
const IDLDepsFile = "idl.deps"

// checkIDLDeps: idl.deps exists and was written for the commit idl.lock
// names. Without the file the deployed binary carries no generations and the
// gate stays off; a stale one would wait for the wrong generations.
func checkIDLDeps(root, idlDir string) []Finding {
	if _, err := os.Stat(filepath.Join(idlDir, ".git")); err != nil {
		return nil // not a checkout; nothing to compare with
	}
	lock, err := os.ReadFile(filepath.Join(root, IDLLockFile))
	if err != nil {
		return nil // idl-lock reports it
	}
	want := strings.TrimSpace(string(lock))
	deps, err := os.ReadFile(filepath.Join(root, IDLDepsFile))
	if err != nil {
		return []Finding{{IDLDepsFile, 0, "idl-deps", "missing: run make gen, which writes idl.deps (the IDL generation of this service and of each RPC service it calls; at start the service waits until the ones it calls are at least that new) next to idl.lock; commit both with the service"}}
	}
	commit, self := "", false
	for n, line := range strings.Split(string(deps), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch {
		case f[0] == "idl" && len(f) == 2:
			commit = f[1]
		case f[0] == "self" && len(f) == 3:
			self = true
		case f[0] == "dep" && len(f) == 3:
		default:
			return []Finding{{IDLDepsFile, n + 1, "idl-deps", fmt.Sprintf("cannot read %q: the file is written by make gen (idl / self / dep lines), not by hand; run make gen", strings.TrimSpace(line))}}
		}
	}
	if commit == "" || !self {
		return []Finding{{IDLDepsFile, 0, "idl-deps", "has no idl or self line: run make gen, which writes the file, and commit it"}}
	}
	if commit != want {
		return []Finding{{IDLDepsFile, 0, "idl-deps", fmt.Sprintf("was written for IDL commit %s but idl.lock names %s: run make gen (it writes both) and commit idl.deps with idl.lock", short(commit), short(want))}}
	}
	return nil
}
