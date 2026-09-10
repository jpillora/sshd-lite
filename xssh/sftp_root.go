package xssh

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/jpillora/sftp"
)

// rootedSFTPServer adapts the forked SFTP request server to os.Root. os.Root keeps
// every filesystem operation beneath one opened directory, including while
// paths are traversed concurrently with symlink or rename changes.
type rootedSFTPServer struct {
	*sftp.RequestServer
	root *os.Root
}

func newRootedSFTPServer(rwc io.ReadWriteCloser, dir string) (*rootedSFTPServer, error) {
	if dir == "" {
		return nil, errors.New("rooted SFTP requires a work directory")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	h := &rootedSFTP{root: root}
	server := sftp.NewRequestServer(rwc, sftp.Handlers{
		FileGet:  h,
		FilePut:  h,
		FileCmd:  h,
		FileList: h,
	}, sftp.WithStartDirectory("/"))
	return &rootedSFTPServer{RequestServer: server, root: root}, nil
}

func (s *rootedSFTPServer) CloseRoot() error { return s.root.Close() }

type rootedSFTP struct{ root *os.Root }

// rootedName converts an SFTP POSIX path into an os.Root-relative native path.
// Prefixing with / gives .. at the virtual root the conventional chroot
// behavior of remaining at that root.
func rootedName(name string) string {
	clean := path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(name, `\`, "/"), "/"))
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" {
		rel = "."
	}
	return filepath.FromSlash(rel)
}

func openFlags(flags sftp.FileOpenFlags) int {
	var value int
	switch {
	case flags.Read && flags.Write:
		value = os.O_RDWR
	case flags.Write:
		value = os.O_WRONLY
	default:
		value = os.O_RDONLY
	}
	if flags.Creat {
		value |= os.O_CREATE
	}
	if flags.Trunc {
		value |= os.O_TRUNC
	}
	if flags.Excl {
		value |= os.O_EXCL
	}
	// O_APPEND conflicts with the WriterAt contract. SFTP clients provide the
	// write offset, matching the ordinary SFTP server behavior.
	return value
}

func createMode(r *sftp.Request, fallback fs.FileMode) fs.FileMode {
	if r.AttrFlags().Permissions {
		return r.Attributes().FileMode().Perm()
	}
	return fallback
}

func (h *rootedSFTP) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return h.root.Open(rootedName(r.Filepath))
}

func (h *rootedSFTP) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return h.root.OpenFile(rootedName(r.Filepath), openFlags(r.Pflags()), createMode(r, 0o644))
}

func (h *rootedSFTP) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	return h.root.OpenFile(rootedName(r.Filepath), openFlags(r.Pflags()), createMode(r, 0o644))
}

func (h *rootedSFTP) Filecmd(r *sftp.Request) error {
	name := rootedName(r.Filepath)
	switch r.Method {
	case "Setstat":
		return h.setstat(name, r)
	case "Rename":
		if _, err := h.root.Lstat(rootedName(r.Target)); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return h.root.Rename(name, rootedName(r.Target))
	case "Rmdir":
		info, err := h.root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return syscall.ENOTDIR
		}
		return h.root.Remove(name)
	case "Remove":
		info, err := h.root.Lstat(name)
		if err != nil {
			return err
		}
		if info.IsDir() {
			return syscall.EISDIR
		}
		return h.root.Remove(name)
	case "Mkdir":
		return h.root.Mkdir(name, createMode(r, 0o755))
	case "Link":
		return h.root.Link(name, rootedName(r.Target))
	case "Symlink":
		target, err := rootedSymlinkTarget(r.Filepath, r.Target)
		if err != nil {
			return err
		}
		return h.root.Symlink(filepath.FromSlash(target), rootedName(r.Target))
	default:
		return errors.New("unsupported SFTP command")
	}
}

func (h *rootedSFTP) PosixRename(r *sftp.Request) error {
	return h.root.Rename(rootedName(r.Filepath), rootedName(r.Target))
}

func (h *rootedSFTP) setstat(name string, r *sftp.Request) error {
	flags, attrs := r.AttrFlags(), r.Attributes()
	openFlag := os.O_RDONLY
	if flags.Size {
		openFlag = os.O_WRONLY
	}
	file, err := h.root.OpenFile(name, openFlag, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	// Apply metadata through the already-resolved handle. On Unix, the
	// path-based os.Root metadata methods are documented as vulnerable to a
	// symlink swap between validation and use.
	if flags.Size {
		if err := file.Truncate(int64(attrs.Size)); err != nil {
			return err
		}
	}
	if flags.Permissions {
		if err := file.Chmod(attrs.FileMode()); err != nil {
			return err
		}
	}
	if flags.UidGid {
		if err := file.Chown(int(attrs.UID), int(attrs.GID)); err != nil {
			return err
		}
	}
	if flags.Acmodtime {
		if err := setFileTimes(h.root, file, name, attrs.AccessTime(), attrs.ModTime()); err != nil {
			return err
		}
	}
	return nil
}

// Symlink requests carry the link contents in Filepath and the link name in
// Target. Convert both absolute and relative client targets into a relative
// link which resolves within the virtual root. Parent traversal clamps at the
// virtual root, matching rootedName and normal chroot path behavior.
func rootedSymlinkTarget(target, link string) (string, error) {
	target = strings.ReplaceAll(target, `\`, "/")
	linkDir := path.Dir("/" + strings.TrimPrefix(strings.ReplaceAll(link, `\`, "/"), "/"))
	var resolved string
	if path.IsAbs(target) {
		resolved = path.Clean(target)
	} else {
		resolved = path.Clean(path.Join(linkDir, target))
	}
	if resolved == "/.." || strings.HasPrefix(resolved, "/../") {
		return "", fs.ErrPermission
	}
	rel, err := filepath.Rel(filepath.FromSlash(linkDir), filepath.FromSlash(resolved))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

type sftpLister []os.FileInfo

func (l sftpLister) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(dst, l[offset:])
	if n < len(dst) {
		return n, io.EOF
	}
	return n, nil
}

// rootedDirLister keeps the directory object alive for the lifetime of its
// SFTP handle. Besides READDIR, the internal request server can therefore give
// FSTAT and FSETSTAT their required open-handle semantics.
type rootedDirLister struct {
	file    *os.File
	entries sftpLister
}

func (l *rootedDirLister) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	return l.entries.ListAt(dst, offset)
}
func (l *rootedDirLister) Stat() (os.FileInfo, error) { return l.file.Stat() }
func (l *rootedDirLister) Truncate(size int64) error  { return l.file.Truncate(size) }
func (l *rootedDirLister) Chmod(mode os.FileMode) error {
	return l.file.Chmod(mode)
}
func (l *rootedDirLister) Chown(uid, gid int) error { return l.file.Chown(uid, gid) }
func (l *rootedDirLister) Fd() uintptr              { return l.file.Fd() }
func (l *rootedDirLister) Close() error             { return l.file.Close() }

func (h *rootedSFTP) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	name := rootedName(r.Filepath)
	switch r.Method {
	case "List":
		dir, err := h.root.Open(name)
		if err != nil {
			return nil, err
		}
		entries, err := dir.Readdir(-1)
		if err != nil {
			dir.Close()
			return nil, err
		}
		return &rootedDirLister{file: dir, entries: sftpLister(entries)}, nil
	case "Stat":
		info, err := h.root.Stat(name)
		if err != nil {
			return nil, err
		}
		return sftpLister{info}, nil
	default:
		return nil, errors.New("unsupported SFTP list command")
	}
}

func (h *rootedSFTP) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	info, err := h.root.Lstat(rootedName(r.Filepath))
	if err != nil {
		return nil, err
	}
	return sftpLister{info}, nil
}

func (h *rootedSFTP) RealPath(name string) (string, error) {
	return path.Clean("/" + strings.TrimPrefix(strings.ReplaceAll(name, `\`, "/"), "/")), nil
}

func (h *rootedSFTP) Readlink(name string) (string, error) {
	target, err := h.root.Readlink(rootedName(name))
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(target), nil
}
