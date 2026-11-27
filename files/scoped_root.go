package files

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

// boundName returns an OS-relative name beneath this filesystem's root.
// Existing absolute links within the scope remain usable: translate their
// resolved path to a relative name, then enforce containment again at use.
func cleanScopedName(name string) (string, error) {
	name = filepath.Clean(name)
	if filepath.VolumeName(name) != "" {
		return "", os.ErrPermission
	}
	name = strings.TrimLeft(name, string(filepath.Separator))
	if name == "" {
		name = "."
	}
	if !filepath.IsLocal(name) {
		return "", os.ErrPermission
	}
	return name, nil
}

func (s *ScopedFs) boundName(name string, followFinal bool) (string, error) {
	name, err := cleanScopedName(name)
	if err != nil {
		return "", err
	}
	if err := s.guard(name); err != nil {
		return "", err
	}
	if name == "." {
		return name, nil
	}

	base, err := filepath.EvalSymlinks(afero.FullBaseFsPath(s.base, "."))
	if err != nil {
		return "", err
	}
	candidate, suffix := name, ""
	if !followFinal {
		candidate, suffix = filepath.Dir(name), filepath.Base(name)
	}
	for {
		resolved, err := filepath.EvalSymlinks(filepath.Join(base, candidate))
		if err == nil {
			rel, err := filepath.Rel(base, resolved)
			if err != nil || !filepath.IsLocal(rel) {
				return "", os.ErrPermission
			}
			return filepath.Join(rel, suffix), nil
		}
		if !errors.Is(err, os.ErrNotExist) || candidate == "." {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(candidate), suffix)
		candidate = filepath.Dir(candidate)
	}
}

// openBoundary opens the actual root used by all OS operations. Nested shares
// open their root through the parent's handle, never through an unchecked
// process-wide path. A scope always names a directory.
func (s *ScopedFs) openBoundary() (*os.Root, error) {
	if s.parent == nil {
		return os.OpenRoot(afero.FullBaseFsPath(s.base, "."))
	}
	parent, err := s.parent.openBoundary()
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	name, err := s.parent.boundName(s.subscope, true)
	if err != nil {
		return nil, err
	}
	return parent.OpenRoot(name)
}

func boundOperation[T any](s *ScopedFs, name string, followFinal bool, op func(*os.Root, string) (T, error)) (T, error) {
	var zero T
	root, err := s.openBoundary()
	if err != nil {
		return zero, err
	}
	defer root.Close()
	name, err = s.boundName(name, followFinal)
	if err != nil {
		return zero, err
	}
	return op(root, name)
}

func (s *ScopedFs) mutate(name string, followFinal bool, op func(*os.Root, string) error) error {
	_, err := boundOperation(s, name, followFinal, func(root *os.Root, name string) (struct{}, error) {
		return struct{}{}, op(root, name)
	})
	return err
}
