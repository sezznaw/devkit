package lint

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IDLLockFile is written by `make gen`: the commit of the IDL checkout the
// service's generated code came from. CI builds against exactly that
// commit, so a build is reproducible and a colleague's IDL change does not
// slip into someone else's build. It is committed with the service.
const IDLLockFile = "idl.lock"

// checkIDLLock: idl.lock exists, matches the IDL checkout, and the commit
// it names has been pushed (CI clones the IDL from the server; an unpushed
// commit fails there, far from the person who could fix it).
func checkIDLLock(root, idlDir string) []Finding {
	if _, err := os.Stat(filepath.Join(idlDir, ".git")); err != nil {
		return nil // not a checkout (CI's shallow clone has one; a plain copy does not)
	}
	lock, err := os.ReadFile(filepath.Join(root, IDLLockFile))
	if err != nil {
		return []Finding{{IDLLockFile, 0, "idl-lock", "missing: run make gen, which records the IDL commit the code was generated from; commit idl.lock with the service"}}
	}
	want := strings.TrimSpace(string(lock))
	head, err := git(idlDir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	var out []Finding
	if want != head {
		out = append(out, Finding{IDLLockFile, 0, "idl-lock", fmt.Sprintf("names IDL commit %s but the checkout is at %s: run make gen (and commit idl.lock) so the code and the lock match the IDL", short(want), short(head))})
	}
	if dirty, _ := git(idlDir, "status", "--porcelain"); strings.TrimSpace(dirty) != "" {
		out = append(out, Finding{IDLLockFile, 0, "idl-lock", "the IDL checkout has uncommitted changes: commit and push the IDL first (the service's CI builds against the IDL on the server)"})
	}
	if contains, _ := git(idlDir, "branch", "-r", "--contains", want); strings.TrimSpace(contains) == "" {
		out = append(out, Finding{IDLLockFile, 0, "idl-lock", fmt.Sprintf("IDL commit %s is not on the server yet: push the idl repository before pushing this service, or its CI cannot fetch the IDL", short(want))})
	}
	return out
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	return strings.TrimSpace(string(b)), err
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
