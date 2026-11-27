package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/afero"
)

// ScopedFs is an afero.Fs that confines every operation to a base directory and
// refuses to follow a symbolic link whose on-disk target resolves outside that
// base. It wraps an *afero.BasePathFs — which already provides the lexical
// confinement — and adds a per-operation scope check on every call that would
// dereference a symlink at the OS layer (open, stat, lstat, chmod, …).
type ScopedFs struct {
	base     *afero.BasePathFs
	parent   *ScopedFs
	subscope string
}

var (
	_ afero.Fs      = (*ScopedFs)(nil)
	_ afero.Lstater = (*ScopedFs)(nil)
)

// maxSymlinkHops bounds how many dangling symlinks within() will follow before
// giving up, so a pathological chain cannot loop forever. It mirrors the kernel
// MAXSYMLINKS limit; the operation is rejected once the bound is exceeded.
const maxSymlinkHops = 255

func NewScopedFs(source afero.Fs, path string) *ScopedFs {
	var parent *ScopedFs
	if s, ok := source.(*ScopedFs); ok {
		parent = s
		source = s.base
	}
	return &ScopedFs{base: afero.NewBasePathFs(source, path).(*afero.BasePathFs), parent: parent, subscope: path}
}

// NewFs builds a user filesystem rooted at path. When followExternal is true it
// returns a bare BasePathFs, so symlinks whose target resolves outside the scope
// are followed; otherwise it returns a ScopedFs that refuses to follow them.
func NewFs(source afero.Fs, path string, followExternal bool) afero.Fs {
	if followExternal {
		return afero.NewBasePathFs(source, path)
	}
	return NewScopedFs(source, path)
}

// BasePath returns the underlying *afero.BasePathFs of a user filesystem built
// by NewFs, whether it is a *ScopedFs or a bare *afero.BasePathFs, or nil if it
// is neither.
func BasePath(fs afero.Fs) *afero.BasePathFs {
	switch f := fs.(type) {
	case *ScopedFs:
		return f.BasePathFs()
	case *afero.BasePathFs:
		return f
	}
	return nil
}

// BasePathFs returns the underlying *afero.BasePathFs.
func (s *ScopedFs) BasePathFs() *afero.BasePathFs { return s.base }

// RealPath resolves a scoped path to the real on-disk path by delegating to
// the underlying BasePathFs. This is needed by callers that need the actual
// filesystem path (e.g. disk.UsageWithContext).
func (s *ScopedFs) RealPath(name string) (string, error) {
	local, err := cleanScopedName(name)
	if err != nil {
		return "", err
	}
	return afero.FullBaseFsPath(s.base, local), nil
}

// guard returns an error if name's on-disk target resolves outside the scope.
func (s *ScopedFs) guard(name string) error {
	ok, err := s.within(name)
	if err != nil {
		return err
	}
	if !ok {
		return os.ErrPermission
	}
	return nil
}

// within reports whether the on-disk target of p — after resolving any symbolic
// links — stays within the scoped root. It exists to stop a symlink that lives
// lexically inside the scope but points outside it from being followed for
// reads, writes, or shares.
//
// Paths that do not exist yet (e.g. a brand-new file being created) are
// validated against their nearest existing ancestor, so legitimate new files
// are always allowed. A dangling symlink — a link whose target does not exist
// yet — is the exception: it is followed to where it points and validated
// there, so a write cannot dereference the link to create a file outside the
// scope.
func (s *ScopedFs) within(p string) (bool, error) {
	root, err := filepath.EvalSymlinks(afero.FullBaseFsPath(s.base, "."))
	if err != nil {
		return false, err
	}

	target := afero.FullBaseFsPath(s.base, p)
	resolved, err := filepath.EvalSymlinks(target)
	// When target does not resolve, work out where the operation would actually
	// land. A non-existent regular path resolves to the file that would be
	// created inside its containing directory, so walk up to the nearest
	// existing ancestor and validate that. But when target itself is a dangling
	// symlink, follow it one level instead: validating its lexical parent would
	// wrongly accept a link pointing outside the scope, letting a write follow
	// the link and create the file out of bounds.
	for hops := 0; errors.Is(err, fs.ErrNotExist); {
		if fi, lerr := os.Lstat(target); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			hops++
			if hops > maxSymlinkHops {
				return false, os.ErrPermission
			}
			dest, rerr := os.Readlink(target)
			if rerr != nil {
				return false, rerr
			}
			if !filepath.IsAbs(dest) {
				// Resolve the link relative to the directory that really contains
				// it, not its lexical parent: a symlinked ancestor could otherwise
				// shift the computed target back into scope while the real write
				// lands outside it. The parent is guaranteed to resolve here
				// because os.Lstat above already traversed it.
				base, berr := filepath.EvalSymlinks(filepath.Dir(target))
				if berr != nil {
					return false, berr
				}
				dest = filepath.Join(base, dest)
			}
			target = filepath.Clean(dest)
		} else {
			parent := filepath.Dir(target)
			if parent == target {
				break
			}
			target = parent
		}
		resolved, err = filepath.EvalSymlinks(target)
	}
	if err != nil {
		return false, err
	}

	// Compare against root with a trailing separator so a sibling like
	// "/srvother" is not treated as being inside "/srv". When root is itself the
	// filesystem boundary (e.g. "/"), it already ends in a separator, so avoid
	// producing "//" — which no path would match — and accept any path under it.
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}

	return resolved == root || strings.HasPrefix(resolved, prefix), nil
}

func (s *ScopedFs) Create(name string) (afero.File, error) {
	return s.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
}

func (s *ScopedFs) Mkdir(name string, perm os.FileMode) error {
	return s.mutate(name, false, func(root *os.Root, name string) error { return root.Mkdir(name, perm.Perm()) })
}

func (s *ScopedFs) MkdirAll(path string, perm os.FileMode) error {
	return s.mutate(path, true, func(root *os.Root, name string) error { return root.MkdirAll(name, perm.Perm()) })
}

func (s *ScopedFs) Open(name string) (afero.File, error) {
	return s.OpenFile(name, os.O_RDONLY, 0)
}

func (s *ScopedFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f, err := boundOperation(s, name, true, func(root *os.Root, name string) (*os.File, error) {
		return root.OpenFile(name, flag, perm.Perm())
	})
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *ScopedFs) Remove(name string) error {
	return s.mutate(name, false, (*os.Root).Remove)
}

func (s *ScopedFs) RemoveAll(path string) error {
	return s.mutate(path, false, (*os.Root).RemoveAll)
}

func (s *ScopedFs) Rename(oldname, newname string) error {
	return s.mutate(".", false, func(root *os.Root, _ string) error {
		oldname, err := s.boundName(oldname, false)
		if err != nil {
			return err
		}
		newname, err := s.boundName(newname, false)
		if err != nil {
			return err
		}
		return root.Rename(oldname, newname)
	})
}

func (s *ScopedFs) Stat(name string) (os.FileInfo, error) {
	return boundOperation(s, name, true, (*os.Root).Stat)
}

func (s *ScopedFs) Name() string { return "ScopedFs" }

func (s *ScopedFs) Chmod(name string, mode os.FileMode) error {
	return s.mutate(name, true, func(root *os.Root, name string) error { return root.Chmod(name, mode) })
}

func (s *ScopedFs) Chown(name string, uid, gid int) error {
	return s.mutate(name, true, func(root *os.Root, name string) error { return root.Chown(name, uid, gid) })
}

func (s *ScopedFs) Chtimes(name string, atime, mtime time.Time) error {
	return s.mutate(name, true, func(root *os.Root, name string) error { return root.Chtimes(name, atime, mtime) })
}

func (s *ScopedFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	info, err := boundOperation(s, name, false, (*os.Root).Lstat)
	return info, true, err
}
