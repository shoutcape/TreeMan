package deps

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ValidateReusePaths validates repository-relative reuse selections.
func ValidateReusePaths(paths []string) error {
	seen := make(map[string]struct{}, len(paths))
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
			return fmt.Errorf("invalid reuse path %q", path)
		}
		parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
		if len(parts) == 0 {
			return fmt.Errorf("invalid reuse path %q", path)
		}
		for _, part := range parts {
			if part == ".." || strings.EqualFold(part, ".git") {
				return fmt.Errorf("invalid reuse path %q", path)
			}
		}
		clean := filepath.Clean(path)
		if clean == "." {
			return fmt.Errorf("invalid reuse path %q", path)
		}
		if _, ok := seen[clean]; ok {
			return fmt.Errorf("duplicate reuse path %q", clean)
		}
		seen[clean] = struct{}{}
		cleaned = append(cleaned, clean)
	}
	for i, path := range cleaned {
		for j, other := range cleaned {
			if i != j && strings.HasPrefix(other, path+string(filepath.Separator)) {
				return fmt.Errorf("overlapping reuse paths %q and %q", path, other)
			}
		}
	}
	return nil
}

// ReuseResult describes how a reuse attempt ended without an error.
type ReuseResult int

const (
	// ReuseCopied means the directory was copied into the destination.
	ReuseCopied ReuseResult = iota + 1
	// ReuseSourceMissing means the main worktree has no such directory.
	ReuseSourceMissing
	// ReuseDestinationExists means the destination already has an entry,
	// which reuse never replaces.
	ReuseDestinationExists
)

// Reuse copies path from sourceRoot to destinationRoot without sharing files.
func Reuse(sourceRoot, destinationRoot, path string) (ReuseResult, error) {
	if err := ValidateReusePaths([]string{path}); err != nil {
		return 0, err
	}
	srcRoot, err := openRoot(sourceRoot)
	if err != nil {
		return 0, fmt.Errorf("open source root: %w", err)
	}
	defer srcRoot.Close()
	dstRoot, err := openRoot(destinationRoot)
	if err != nil {
		return 0, fmt.Errorf("open destination root: %w", err)
	}
	defer dstRoot.Close()
	src, err := openDir(srcRoot.Fd(), path)
	if errors.Is(err, unix.ENOENT) {
		return ReuseSourceMissing, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open source %q: %w", path, err)
	}
	defer src.Close()
	var srcStat unix.Stat_t
	if err := unix.Fstat(int(src.Fd()), &srcStat); err != nil {
		return 0, err
	}
	if srcStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return 0, fmt.Errorf("source %q is not a directory", path)
	}
	sourcePath, err := filepath.EvalSymlinks(filepath.Join(sourceRoot, path))
	if err != nil {
		return 0, fmt.Errorf("resolve source overlap: %w", err)
	}
	destinationPath, err := filepath.EvalSymlinks(destinationRoot)
	if err != nil {
		return 0, fmt.Errorf("resolve destination overlap: %w", err)
	}
	destinationTarget := filepath.Join(destinationPath, filepath.Clean(path))
	if pathsOverlap(sourcePath, destinationTarget) {
		return 0, errors.New("source and destination trees overlap")
	}

	parentPath, name := filepath.Split(filepath.Clean(path))
	parentPath = filepath.Clean(parentPath)
	parent, made, err := openOrCreateDir(int(dstRoot.Fd()), parentPath)
	if err != nil {
		return 0, fmt.Errorf("open destination parent: %w", err)
	}
	defer parent.Close()
	var dstStat unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &dstStat, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if cleanErr := discardMade(int(dstRoot.Fd()), made); cleanErr != nil {
			return 0, fmt.Errorf("destination exists (cleanup: %w)", cleanErr)
		}
		return ReuseDestinationExists, nil
	} else if !errors.Is(err, unix.ENOENT) {
		if cleanErr := discardMade(int(dstRoot.Fd()), made); cleanErr != nil {
			return 0, cleanErr
		}
		return 0, err
	}
	if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil {
		if errors.Is(err, unix.EEXIST) {
			if cleanErr := discardMade(int(dstRoot.Fd()), made); cleanErr != nil {
				return 0, fmt.Errorf("destination exists (cleanup: %w)", cleanErr)
			}
			return ReuseDestinationExists, nil
		}
		if cleanErr := discardMade(int(dstRoot.Fd()), made); cleanErr != nil {
			return 0, cleanErr
		}
		return 0, err
	}
	dst, err := openDir(parent.Fd(), name)
	if err != nil {
		// Without an opened descriptor we cannot verify ownership of the entry.
		err = fmt.Errorf("open destination: %w (cleanup: could not verify ownership of %q)", err, name)
		if cleanErr := discardMade(int(dstRoot.Fd()), made); cleanErr != nil {
			return 0, fmt.Errorf("open destination: %w (cleanup: %v)", err, cleanErr)
		}
		return 0, err
	}
	defer dst.Close()
	state := newCleanupState(dstRoot, path)
	for _, m := range made {
		state.addPath(m.path, true, m.dev, m.ino)
	}
	var createdStat unix.Stat_t
	if err := unix.Fstat(int(dst.Fd()), &createdStat); err != nil {
		return 0, state.fail(err)
	}
	rootEntry := cleanupEntry{parent: parentPath, name: name, dir: true, dev: uint64(createdStat.Dev), ino: uint64(createdStat.Ino)}
	state.entries = append(state.entries, rootEntry)
	state.dirs[filepath.Join(parentPath, name)] = rootEntry
	nodes := map[string]node{}
	if err := snapshot(int(src.Fd()), "", nodes); err != nil {
		return 0, state.fail(fmt.Errorf("validate source: %w", err))
	}
	if err := validateLinks(nodes); err != nil {
		return 0, state.fail(err)
	}
	if _, err := unix.Seek(int(src.Fd()), 0, 0); err != nil {
		return 0, state.fail(fmt.Errorf("rewind source: %w", err))
	}
	if err := copyTree(int(src.Fd()), int(dst.Fd()), "", state); err != nil {
		return 0, state.fail(err)
	}
	copiedNodes := map[string]node{}
	if err := snapshot(int(dst.Fd()), "", copiedNodes); err != nil {
		return 0, state.fail(fmt.Errorf("validate copied tree: %w", err))
	}
	if err := validateLinks(copiedNodes); err != nil {
		return 0, state.fail(fmt.Errorf("validate copied tree: %w", err))
	}
	if err := unix.Fchmod(int(dst.Fd()), uint32(srcStat.Mode&0o777)); err != nil {
		return 0, state.fail(err)
	}
	return ReuseCopied, nil
}

type node struct {
	mode uint32
	link string
}
type madeDir struct {
	path     string
	dev, ino uint64
}
type cleanupEntry struct {
	parent, name string
	dir          bool
	dev, ino     uint64
}
type cleanupState struct {
	anchor    *os.File
	prefix    string
	entries   []cleanupEntry
	dirs      map[string]cleanupEntry
	ownership []error
}

// Store identities rather than one open descriptor per copied directory. Large
// dependency trees can otherwise exhaust the process's file descriptor limit.
func newCleanupState(anchor *os.File, prefix string) *cleanupState {
	return &cleanupState{anchor: anchor, prefix: filepath.Clean(prefix), dirs: make(map[string]cleanupEntry)}
}
func (s *cleanupState) parentPath(rel string) string {
	if rel == "" || rel == "." {
		return s.prefix
	}
	return filepath.Join(s.prefix, rel)
}

// addOpened records an entry by the descriptor this attempt opened for it, so
// a concurrent swap of the name cannot make cleanup claim someone else's entry.
func (s *cleanupState) addOpened(parentPath, name string, dir bool, fd int) error {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		ownErr := fmt.Errorf("cleanup ownership for %s/%s: %w", parentPath, name, err)
		s.ownership = append(s.ownership, ownErr)
		return ownErr
	}
	parentPath = s.parentPath(parentPath)
	e := cleanupEntry{parent: parentPath, name: name, dir: dir, dev: uint64(st.Dev), ino: uint64(st.Ino)}
	s.entries = append(s.entries, e)
	if dir {
		s.dirs[filepath.Join(parentPath, name)] = e
	}
	return nil
}

// add records a symlink by name; a symlink cannot be opened portably.
func (s *cleanupState) add(parentPath, name string, parent int) error {
	var st unix.Stat_t
	if err := unix.Fstatat(parent, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		ownErr := fmt.Errorf("cleanup ownership for %s/%s: %w", parentPath, name, err)
		s.ownership = append(s.ownership, ownErr)
		return ownErr
	}
	parentPath = s.parentPath(parentPath)
	s.entries = append(s.entries, cleanupEntry{parent: parentPath, name: name, dev: uint64(st.Dev), ino: uint64(st.Ino)})
	return nil
}
func (s *cleanupState) addPath(path string, dir bool, dev, ino uint64) {
	parent, name := filepath.Split(path)
	e := cleanupEntry{parent: filepath.Clean(parent), name: name, dir: dir, dev: dev, ino: ino}
	s.entries = append(s.entries, e)
	if dir {
		s.dirs[filepath.Clean(path)] = e
	}
}
func (s *cleanupState) fail(cause error) error {
	var clean error
	for _, err := range s.ownership {
		if errors.Is(cause, err) {
			continue // Already reported as the cause.
		}
		clean = errors.Join(clean, err)
	}
	// Restore permissions top-down before removing children of read-only dirs.
	for _, e := range s.entries {
		if !e.dir {
			continue
		}
		parent, err := s.openParent(e.parent)
		if err != nil {
			if !errors.Is(err, unix.ENOENT) {
				clean = errors.Join(clean, err)
			}
			continue
		}
		var current unix.Stat_t
		if err := unix.Fstatat(int(parent.Fd()), e.name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			parent.Close()
			if !errors.Is(err, unix.ENOENT) {
				clean = errors.Join(clean, err)
			}
			continue
		}
		if uint64(current.Dev) != e.dev || uint64(current.Ino) != e.ino {
			parent.Close()
			continue
		}
		dir, err := openDir(parent.Fd(), e.name)
		if err == nil {
			err = unix.Fchmod(int(dir.Fd()), 0o700)
			dir.Close()
		}
		parent.Close()
		if err != nil && !errors.Is(err, unix.ENOENT) {
			clean = errors.Join(clean, err)
		}
	}
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		parent, err := s.openParent(e.parent)
		if err != nil {
			if !errors.Is(err, unix.ENOENT) {
				clean = errors.Join(clean, err)
			}
			continue
		}
		var current unix.Stat_t
		if err := unix.Fstatat(int(parent.Fd()), e.name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			parent.Close()
			if !errors.Is(err, unix.ENOENT) {
				clean = errors.Join(clean, err)
			}
			continue
		}
		if uint64(current.Dev) != e.dev || uint64(current.Ino) != e.ino {
			parent.Close()
			continue
		}
		flags := 0
		if e.dir {
			flags = unix.AT_REMOVEDIR
		}
		if err := unix.Unlinkat(int(parent.Fd()), e.name, flags); err != nil && !errors.Is(err, unix.ENOENT) {
			clean = errors.Join(clean, err)
		}
		parent.Close()
	}
	if clean != nil {
		return fmt.Errorf("%w (cleanup: %v)", cause, clean)
	}
	return cause
}

func (s *cleanupState) openParent(path string) (*os.File, error) {
	fd, err := unix.Dup(int(s.anchor.Fd()))
	if err != nil {
		return nil, err
	}
	cur := os.NewFile(uintptr(fd), "cleanup-parent")
	built := "."
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next, err := unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			cur.Close()
			return nil, err
		}
		old := cur
		cur = os.NewFile(uintptr(next), "cleanup-parent")
		old.Close()
		if built == "." {
			built = part
		} else {
			built = filepath.Join(built, part)
		}
		if expected, ok := s.dirs[filepath.Clean(built)]; ok {
			var st unix.Stat_t
			if err := unix.Fstat(int(cur.Fd()), &st); err != nil || uint64(st.Dev) != expected.dev || uint64(st.Ino) != expected.ino {
				cur.Close()
				if err == nil {
					err = errors.New("cleanup ancestor changed")
				}
				return nil, err
			}
		}
	}
	return cur, nil
}

func discardMade(root int, made []madeDir) error {
	var clean error
	for i := len(made) - 1; i >= 0; i-- {
		parentPath, name := filepath.Split(made[i].path)
		parent, err := openDir(uintptr(root), filepath.Clean(parentPath))
		if err == nil {
			var st unix.Stat_t
			statErr := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
			if statErr == nil && uint64(st.Dev) == made[i].dev && uint64(st.Ino) == made[i].ino {
				if dir, err := openDir(parent.Fd(), name); err == nil {
					if err := unix.Fchmod(int(dir.Fd()), 0o700); err != nil {
						clean = errors.Join(clean, err)
					}
					dir.Close()
					if err := unix.Unlinkat(int(parent.Fd()), name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
						clean = errors.Join(clean, err)
					}
				} else if !errors.Is(err, unix.ENOENT) {
					clean = errors.Join(clean, err)
				}
			} else if statErr != nil && !errors.Is(statErr, unix.ENOENT) {
				clean = errors.Join(clean, statErr)
			}
			parent.Close()
		} else if !errors.Is(err, unix.ENOENT) {
			clean = errors.Join(clean, err)
		}
	}
	return clean
}

func openRoot(path string) (*os.File, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return nil, err
	}
	return openDirPath(abs)
}
func openDirPath(path string) (*os.File, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		f.Close()
		if err == nil {
			err = unix.ENOTDIR
		}
		return nil, err
	}
	return f, nil
}
func openDir(rootFd uintptr, path string) (*os.File, error) {
	fd, err := unix.Dup(int(rootFd))
	if err != nil {
		return nil, err
	}
	cur := os.NewFile(uintptr(fd), "reuse-dir")
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		next, err := unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			cur.Close()
			return nil, err
		}
		old := cur
		cur = os.NewFile(uintptr(next), "reuse-dir")
		old.Close()
	}
	return cur, nil
}

// beforeParentMkdir lets tests create a parent directory concurrently, in the
// window between a missing-parent open and its Mkdirat.
var beforeParentMkdir = func(dirFd int, name string) {}

func openOrCreateDir(root int, path string) (*os.File, []madeDir, error) {
	// The returned file owns only a duplicate, never the caller's descriptor.
	fd, err := unix.Dup(root)
	if err != nil {
		return nil, nil, err
	}
	cur := os.NewFile(uintptr(fd), "reuse-parent")
	var made []madeDir
	built := "."
	if path == "." {
		return cur, made, nil
	}
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		nextPath := part
		if built != "." {
			nextPath = filepath.Join(built, part)
		}
		next, e := unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) {
			beforeParentMkdir(int(cur.Fd()), part)
			e = unix.Mkdirat(int(cur.Fd()), part, 0o755)
			if errors.Is(e, unix.EEXIST) {
				// Another process created it first. Use it, but do not record
				// it as ours, so cleanup never removes it.
				next, e = unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			} else if e == nil {
				next, e = unix.Openat(int(cur.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if e == nil {
					var st unix.Stat_t
					e = unix.Fstat(next, &st)
					if e == nil {
						made = append(made, madeDir{path: nextPath, dev: uint64(st.Dev), ino: uint64(st.Ino)})
					} else {
						unix.Close(next)
					}
				}
				if e != nil {
					e = fmt.Errorf("%w (cleanup: could not verify ownership of created parent %q)", e, nextPath)
				}
			}
		}
		if e != nil {
			cur.Close()
			if cleanErr := discardMade(root, made); cleanErr != nil {
				e = errors.Join(e, fmt.Errorf("cleanup created parents: %w", cleanErr))
			}
			return nil, made, e
		}
		built = nextPath
		old := cur
		cur = os.NewFile(uintptr(next), "reuse-parent")
		old.Close()
	}
	return cur, made, nil
}

func snapshot(fd int, rel string, nodes map[string]node) error {
	dir, err := dupFile(fd, "snapshot")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := name
		if rel != "" {
			path = filepath.Join(rel, name)
		}
		var st unix.Stat_t
		if err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		mode := uint32(st.Mode & unix.S_IFMT)
		switch mode {
		case unix.S_IFDIR:
			child, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			err = snapshot(child, path, nodes)
			unix.Close(child)
			if err != nil {
				return err
			}
			nodes[path] = node{mode: mode}
		case unix.S_IFREG:
			nodes[path] = node{mode: mode}
		case unix.S_IFLNK:
			link, err := readlinkat(fd, name)
			if err != nil {
				return err
			}
			nodes[path] = node{mode: mode, link: link}
		default:
			return fmt.Errorf("unsupported entry %q", path)
		}
	}
	return nil
}

func validateLinks(nodes map[string]node) error {
	for path, n := range nodes {
		if n.mode == unix.S_IFLNK {
			if _, err := resolve(path, nodes); err != nil {
				return fmt.Errorf("unsafe symlink %q: %w", path, err)
			}
		}
	}
	return nil
}
func resolve(path string, nodes map[string]node) (string, error) {
	// Expand each link before processing subsequent '..' components. Cleaning
	// the full target first would miss escapes through directory aliases.
	queue := strings.Split(filepath.ToSlash(path), "/")
	out := []string{}
	expansions := 0
	for len(queue) > 0 {
		part := queue[0]
		queue = queue[1:]
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(out) == 0 {
				return "", errors.New("escapes selected directory")
			}
			out = out[:len(out)-1]
			continue
		}
		candidate := filepath.Join(append(append([]string{}, out...), part)...)
		n, ok := nodes[candidate]
		if !ok {
			out = append(out, part)
			continue
		}
		if n.mode != unix.S_IFLNK {
			out = append(out, part)
			continue
		}
		expansions++
		if expansions > 40 {
			return "", errors.New("symlink cycle or excessive expansion")
		}
		if filepath.IsAbs(n.link) {
			return "", errors.New("absolute target")
		}
		prefix := strings.Split(filepath.ToSlash(n.link), "/")
		queue = append(prefix, queue...)
	}
	return filepath.Join(out...), nil
}
func pathsOverlap(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

func copyTree(src, dst int, rel string, state *cleanupState) error {
	dir, err := dupFile(src, "copy-source")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		path := name
		if rel != "" {
			path = filepath.Join(rel, name)
		}
		var st unix.Stat_t
		if err := unix.Fstatat(src, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		mode := uint32(st.Mode & unix.S_IFMT)
		switch mode {
		case unix.S_IFDIR:
			if err := unix.Mkdirat(dst, name, 0o700); err != nil {
				return err
			}
			out, err := unix.Openat(dst, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				ownErr := fmt.Errorf("cleanup ownership for %s/%s: %w", rel, name, err)
				state.ownership = append(state.ownership, ownErr)
				return ownErr
			}
			if err := state.addOpened(rel, name, true, out); err != nil {
				unix.Close(out)
				return err
			}
			in, err := unix.Openat(src, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				unix.Close(out)
				return err
			}
			err = copyTree(in, out, path, state)
			unix.Close(in)
			if err != nil {
				unix.Close(out)
				return err
			}
			if err := unix.Fchmod(out, uint32(st.Mode&0o777)); err != nil {
				unix.Close(out)
				return err
			}
			unix.Close(out)
		case unix.S_IFREG:
			in, err := unix.Openat(src, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
			if err != nil {
				return err
			}
			inFile := os.NewFile(uintptr(in), "copy-source")
			inInfo, e := inFile.Stat()
			if e != nil || !inInfo.Mode().IsRegular() {
				inFile.Close()
				if e == nil {
					e = errors.New("source changed to non-regular file")
				}
				return e
			}
			out, err := unix.Openat(dst, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC, 0o600)
			if err != nil {
				inFile.Close()
				return err
			}
			outFile := os.NewFile(uintptr(out), "copy-destination")
			if err := state.addOpened(rel, name, false, out); err != nil {
				return errors.Join(err, inFile.Close(), outFile.Close())
			}
			_, e = io.Copy(outFile, inFile)
			e = errors.Join(e, inFile.Close())
			if e == nil {
				e = unix.Fchmod(out, uint32(st.Mode&0o777))
			}
			e = errors.Join(e, outFile.Close())
			if e != nil {
				return e
			}
		case unix.S_IFLNK:
			link, err := readlinkat(src, name)
			if err != nil {
				return err
			}
			if !safeLink(path, link) {
				return fmt.Errorf("unsafe symlink %q", path)
			}
			if err := unix.Symlinkat(link, dst, name); err != nil {
				return err
			}
			if err := state.add(rel, name, dst); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry %q", path)
		}
	}
	return nil
}
func safeLink(path, link string) bool {
	if filepath.IsAbs(link) {
		return false
	}
	target := filepath.Join(filepath.Dir(path), link)
	return target != ".." && !strings.HasPrefix(target, ".."+string(filepath.Separator))
}
func dupFile(fd int, name string) (*os.File, error) {
	n, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(n), name), nil
}
func readlinkat(fd int, name string) (string, error) {
	buf := make([]byte, 65536)
	n, err := unix.Readlinkat(fd, name, buf)
	if err != nil {
		return "", err
	}
	if n == len(buf) {
		return "", errors.New("symlink target too long")
	}
	return string(buf[:n]), nil
}
