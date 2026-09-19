package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sezznaw/devkit/internal/manifest"
	"github.com/sezznaw/devkit/internal/render"
)

// NewSuffix is appended to the incoming version of a file that devkit refused
// to overwrite because the user had modified it.
const NewSuffix = ".new"

// BackupSuffix is appended to a user-modified file before --force overwrites it.
const BackupSuffix = ".bak"

// Result summarises what applyPlan did.
type Result struct {
	Written  []string // created or overwritten
	Skipped  []string // user-modified, left alone (a .new copy was written)
	Deleted  []string // no longer produced by the component
	Kept     []string // no longer produced, but user-modified so kept
	BackedUp []string // .bak files created by --force
}

// applyPlan writes plan into the project, reconciling against the previously
// installed state of the same component (old may be nil for a fresh install).
// It returns the file→hash map to store in the manifest.
func (in *Installer) applyPlan(name string, plan *render.Plan, old *manifest.Installed) (map[string]string, *Result, error) {
	states := map[string]manifest.FileState{}
	if old != nil {
		var err error
		states, err = in.Manifest.CheckFiles(in.Root, name)
		if err != nil {
			return nil, nil, err
		}
	}

	// Pre-pass: files we would create that exist on disk and are not ours.
	var conflicts []string
	for _, f := range plan.Files {
		if _, ours := states[f.Rel]; ours {
			continue
		}
		if _, err := os.Stat(in.abs(f.Rel)); errors.Is(err, os.ErrNotExist) {
			continue
		}
		conflicts = append(conflicts, f.Rel)
	}
	if len(conflicts) > 0 && !in.Force {
		return nil, nil, fmt.Errorf("refusing to overwrite existing files (use --force to overwrite, a .bak copy is kept):\n  %s",
			strings.Join(conflicts, "\n  "))
	}

	res := &Result{}
	hashes := make(map[string]string, len(plan.Files))
	for _, f := range plan.Files {
		abs := in.abs(f.Rel)
		state, ours := states[f.Rel]
		_, exists := os.Stat(abs)
		mustBackup := false
		switch {
		case ours && state == manifest.Modified && !in.Force:
			// Leave the user's file alone; drop the new content next to it.
			if err := writeFile(abs+NewSuffix, f.Content, f.Mode); err != nil {
				return nil, nil, err
			}
			res.Skipped = append(res.Skipped, f.Rel)
			hashes[f.Rel] = manifest.HashBytes(f.Content)
			continue
		case ours && state == manifest.Modified && in.Force:
			mustBackup = true
		case !ours && exists == nil && in.Force:
			mustBackup = true
		}
		if mustBackup {
			if err := os.Rename(abs, abs+BackupSuffix); err != nil {
				return nil, nil, err
			}
			res.BackedUp = append(res.BackedUp, f.Rel+BackupSuffix)
		}
		if err := writeFile(abs, f.Content, f.Mode); err != nil {
			return nil, nil, err
		}
		// A stale .new from an earlier skipped update is now obsolete.
		os.Remove(abs + NewSuffix)
		res.Written = append(res.Written, f.Rel)
		hashes[f.Rel] = manifest.HashBytes(f.Content)
	}

	// Files the previous version produced that the new one does not.
	planned := map[string]bool{}
	for _, f := range plan.Files {
		planned[f.Rel] = true
	}
	var stale []string
	for rel := range states {
		if !planned[rel] {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	for _, rel := range stale {
		switch states[rel] {
		case manifest.Unchanged:
			if err := os.Remove(in.abs(rel)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, nil, err
			}
			pruneEmptyDirs(in.Root, filepath.Dir(in.abs(rel)))
			res.Deleted = append(res.Deleted, rel)
		case manifest.Modified:
			if in.Force {
				if err := os.Rename(in.abs(rel), in.abs(rel)+BackupSuffix); err != nil {
					return nil, nil, err
				}
				res.BackedUp = append(res.BackedUp, rel+BackupSuffix)
				res.Deleted = append(res.Deleted, rel)
			} else {
				res.Kept = append(res.Kept, rel)
			}
		case manifest.Missing:
			// already gone
		}
	}
	return hashes, res, nil
}

func (in *Installer) abs(rel string) string {
	return filepath.Join(in.Root, filepath.FromSlash(rel))
}

func writeFile(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, mode|0o600)
}

// pruneEmptyDirs removes dir and its parents while they are empty, stopping at root.
func pruneEmptyDirs(root, dir string) {
	root = filepath.Clean(root)
	for {
		dir = filepath.Clean(dir)
		if dir == root || !strings.HasPrefix(dir, root+string(os.PathSeparator)) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// report prints a Result in a consistent format.
func (in *Installer) report(res *Result) {
	for _, f := range res.Written {
		in.Log("  + %s", f)
	}
	for _, f := range res.Deleted {
		in.Log("  - %s", f)
	}
	for _, f := range res.Kept {
		in.Log("  ! %s  (no longer part of the component, kept because you modified it)", f)
	}
	for _, f := range res.Skipped {
		in.Log("  ! %s  (modified locally, skipped; new version written to %s%s)", f, f, NewSuffix)
	}
	for _, f := range res.BackedUp {
		in.Log("  ~ %s  (backup)", f)
	}
}
