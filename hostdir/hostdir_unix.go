// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build unix

package hostdir

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func ignoringEINTR(fn func() error) error {
	for {
		err := fn()
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func ignoringEINTRInt(fn func() (int, error)) (int, error) {
	for {
		v, err := fn()
		if !errors.Is(err, unix.EINTR) {
			return v, err
		}
	}
}

func isSymlinkOpenErr(err error) bool {
	return errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK)
}

func validateParentStat(parent string, pst *unix.Stat_t, euid uint32) error {
	pmode := uint32(pst.Mode)
	if (pmode & unix.S_IFMT) != unix.S_IFDIR {
		return fmt.Errorf("parent path %q is not a directory", parent)
	}
	if pst.Uid != 0 && pst.Uid != euid {
		return fmt.Errorf("parent directory %q is owned by untrusted uid %d (want current uid %d or root 0)", parent, pst.Uid, euid)
	}
	if (pmode&0002) != 0 && (pmode&unix.S_ISVTX) == 0 {
		return fmt.Errorf("parent directory %q (mode %#o) is world-writable without sticky bit", parent, pmode&07777)
	}
	if pst.Uid == 0 && (pmode&0020) != 0 && (pmode&unix.S_ISVTX) == 0 {
		return fmt.Errorf("root-owned parent directory %q (mode %#o) is group-writable without sticky bit", parent, pmode&07777)
	}
	return nil
}

func openParentDir(parent string, euid uint32) (int, error) {
	parentFD, err := ignoringEINTRInt(func() (int, error) {
		return unix.Open(parent, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	})
	if err != nil && (isSymlinkOpenErr(err) || errors.Is(err, unix.ENOTDIR)) {
		// On Linux, open(O_NOFOLLOW|O_DIRECTORY) on a symlink returns ENOTDIR
		// rather than ELOOP. Check via Lstat whether parent is actually a symlink.
		// Support OS-managed parent symlinks (such as /tmp -> private/tmp on macOS)
		// only when both the symlink itself and its containing directory are owned
		// by root (0) or the current effective UID.
		var lst unix.Stat_t
		if lerr := ignoringEINTR(func() error { return unix.Lstat(parent, &lst) }); lerr != nil {
			return -1, fmt.Errorf("failed to lstat parent %q: %w", parent, lerr)
		}
		if (uint32(lst.Mode) & unix.S_IFMT) != unix.S_IFLNK {
			return -1, fmt.Errorf("parent path %q is not a directory: %w", parent, err)
		}
		if lst.Uid != 0 && lst.Uid != euid {
			return -1, fmt.Errorf("parent path %q is an untrusted symlink (uid=%d): %w", parent, lst.Uid, err)
		}
		gp := filepath.Dir(parent)
		var gpSt unix.Stat_t
		if gerr := ignoringEINTR(func() error { return unix.Stat(gp, &gpSt) }); gerr != nil {
			return -1, fmt.Errorf("failed to stat grandparent %q: %w", gp, gerr)
		}
		if gerr := validateParentStat(gp, &gpSt, euid); gerr != nil {
			return -1, gerr
		}
		parentFD, err = ignoringEINTRInt(func() (int, error) {
			return unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		})
	}
	if err != nil {
		return -1, fmt.Errorf("failed to open parent directory %q: %w", parent, err)
	}

	var pst unix.Stat_t
	if err := ignoringEINTR(func() error { return unix.Fstat(parentFD, &pst) }); err != nil {
		_ = unix.Close(parentFD)
		return -1, fmt.Errorf("failed to fstat parent directory %q: %w", parent, err)
	}
	if err := validateParentStat(parent, &pst, euid); err != nil {
		_ = unix.Close(parentFD)
		return -1, err
	}
	return parentFD, nil
}

func openVerifiedDir(dir string, create bool) (int, error) {
	if err := validateCleanAbsDir(dir); err != nil {
		return -1, err
	}
	parent := filepath.Dir(dir)
	base := filepath.Base(dir)
	euid := uint32(os.Geteuid())

	parentFD, err := openParentDir(parent, euid)
	if err != nil {
		return -1, err
	}
	defer func() { _ = unix.Close(parentFD) }()

	if create {
		err := ignoringEINTR(func() error {
			return unix.Mkdirat(parentFD, base, 0700)
		})
		if err != nil && !errors.Is(err, unix.EEXIST) {
			return -1, fmt.Errorf("failed to create host directory %q: %w", dir, err)
		}
	}

	fd, err := ignoringEINTRInt(func() (int, error) {
		return unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	})
	if err != nil {
		if !create && errors.Is(err, unix.ENOENT) {
			return -1, os.ErrNotExist
		}
		if isSymlinkOpenErr(err) {
			return -1, fmt.Errorf("host directory %q must not be a symlink: %w", dir, err)
		}
		if errors.Is(err, unix.ENOTDIR) {
			var lst unix.Stat_t
			if lerr := ignoringEINTR(func() error {
				return unix.Fstatat(parentFD, base, &lst, unix.AT_SYMLINK_NOFOLLOW)
			}); lerr == nil && (uint32(lst.Mode)&unix.S_IFMT) == unix.S_IFLNK {
				return -1, fmt.Errorf("host directory %q must not be a symlink: %w", dir, err)
			}
			return -1, fmt.Errorf("host path %q is not a directory: %w", dir, err)
		}
		return -1, fmt.Errorf("failed to open host directory %q: %w", dir, err)
	}

	var st unix.Stat_t
	if err := ignoringEINTR(func() error { return unix.Fstat(fd, &st) }); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("failed to fstat host directory %q: %w", dir, err)
	}
	fix, err := checkHostDir(dir, &st, euid)
	if err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	switch fix {
	case fixPrivate:
		if err := ignoringEINTR(func() error { return unix.Fchmod(fd, 0700) }); err != nil {
			_ = unix.Close(fd)
			return -1, fmt.Errorf("failed to tighten permissions on %q to 0700: %w", dir, err)
		}
	case fixSticky:
		if err := ignoringEINTR(func() error { return unix.Fchmod(fd, stickyWorldWritable) }); err != nil {
			_ = unix.Close(fd)
			return -1, fmt.Errorf("failed to set sticky mode 01777 on %q: %w", dir, err)
		}
	}
	return fd, nil
}

// stickyWorldWritable is the mode of a shared, root-owned host directory
// (like /tmp itself): anyone may create entries, but only an entry's owner
// (or root) may rename or remove it.
const stickyWorldWritable = uint32(unix.S_ISVTX | 0777)

// dirFix is the in-place permission repair openVerifiedDir applies to an
// existing host directory.
type dirFix int

const (
	fixNone    dirFix = iota
	fixPrivate        // chmod 0700: directory owned by the non-root effective UID.
	fixSticky         // chmod 01777: root-owned directory used by root.
)

// checkHostDir reports whether a host directory with the ownership and mode in
// st is safe to use for the effective UID euid, and which in-place permission
// repair, if any, should be applied to it. A directory is safe when no other
// non-root UID can rename or replace its entries.
func checkHostDir(dir string, st *unix.Stat_t, euid uint32) (dirFix, error) {
	mode := uint32(st.Mode)
	if (mode & unix.S_IFMT) != unix.S_IFDIR {
		return fixNone, fmt.Errorf("host path %q is not a directory", dir)
	}
	perm := mode & 07777
	switch {
	case st.Uid == euid && euid != 0:
		if perm != 0700 {
			return fixPrivate, nil
		}
		return fixNone, nil
	case st.Uid == 0 && euid == 0:
		if perm != stickyWorldWritable {
			return fixSticky, nil
		}
		return fixNone, nil
	case st.Uid == 0:
		// Root-owned and used by a non-root UID: the sticky 01777 mode set up
		// by tmpfiles.d on KNE VM images, or a 0755 directory that Docker
		// creates for a missing KinD extraMounts hostPath. Either is safe;
		// only group- or world-writable without the sticky bit is not.
		if perm&0022 != 0 && perm&unix.S_ISVTX == 0 {
			return fixNone, fmt.Errorf("root-owned host directory %q has mode %#o (writable by other users without the sticky bit); repair in place with: sudo chown root:root %q && sudo chmod 1777 %q", dir, perm, dir, dir)
		}
		return fixNone, nil
	default:
		return fixNone, fmt.Errorf("host directory %q is owned by uid %d (want current uid %d or root); repair in place with: sudo chown root:root %q && sudo chmod 1777 %q", dir, st.Uid, euid, dir, dir)
	}
}

// EnsureDir ensures dir exists and is a non-symlink directory that no other
// non-root user can modify: owned by the current effective UID (tightened to
// mode 0700), or owned by root and not writable by others unless sticky (root
// itself sets mode 01777 so that other users can share the directory).
func EnsureDir(dir string) error {
	fd, err := openVerifiedDir(dir, true)
	if err != nil {
		return err
	}
	return unix.Close(fd)
}

// CreateConfigFile writes data into a newly created 0444 file inside a fresh,
// randomly named 0700 subdirectory under baseDir without following symlinks.
func CreateConfigFile(baseDir, nodeName string, data []byte) (string, error) {
	euid := uint32(os.Geteuid())
	dirFD, err := openVerifiedDir(baseDir, true)
	if err != nil {
		return "", err
	}
	defer func() { _ = unix.Close(dirFD) }()

	safeNode := sanitizeNodeName(nodeName)
	var subName string
	var mkdirErr error
	for range 16 {
		var rnd [8]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return "", fmt.Errorf("failed to generate random config directory suffix: %w", err)
		}
		subName = fmt.Sprintf("kne-%s-config-%x", safeNode, rnd)
		mkdirErr = ignoringEINTR(func() error {
			return unix.Mkdirat(dirFD, subName, 0700)
		})
		if mkdirErr == nil {
			break
		}
		if !errors.Is(mkdirErr, unix.EEXIST) {
			if errors.Is(mkdirErr, unix.EACCES) || errors.Is(mkdirErr, unix.EPERM) {
				return "", fmt.Errorf("host directory %q is not writable by uid %d: %w; repair in place with: sudo chown root:root %q && sudo chmod 1777 %q", baseDir, euid, mkdirErr, baseDir, baseDir)
			}
			return "", fmt.Errorf("failed to create config directory %q in %q: %w", subName, baseDir, mkdirErr)
		}
	}
	if mkdirErr != nil {
		return "", fmt.Errorf("exhausted retries creating unique config directory in %q: %w", baseDir, mkdirErr)
	}

	cleanupSubDir := func() {
		_ = ignoringEINTR(func() error {
			return unix.Unlinkat(dirFD, subName, unix.AT_REMOVEDIR)
		})
	}

	subFD, err := ignoringEINTRInt(func() (int, error) {
		return unix.Openat(dirFD, subName, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	})
	if err != nil {
		cleanupSubDir()
		return "", fmt.Errorf("failed to open config directory %q: %w", filepath.Join(baseDir, subName), err)
	}

	var subSt unix.Stat_t
	if err := ignoringEINTR(func() error { return unix.Fstat(subFD, &subSt) }); err != nil {
		_ = unix.Close(subFD)
		cleanupSubDir()
		return "", fmt.Errorf("failed to fstat config directory %q: %w", filepath.Join(baseDir, subName), err)
	}
	if subSt.Uid != euid || (uint32(subSt.Mode)&unix.S_IFMT) != unix.S_IFDIR {
		_ = unix.Close(subFD)
		cleanupSubDir()
		return "", fmt.Errorf("unexpected owner or type on config directory %q (uid=%d, want=%d)", filepath.Join(baseDir, subName), subSt.Uid, euid)
	}
	if (uint32(subSt.Mode) & 07777) != 0700 {
		if err := ignoringEINTR(func() error { return unix.Fchmod(subFD, 0700) }); err != nil {
			_ = unix.Close(subFD)
			cleanupSubDir()
			return "", fmt.Errorf("failed to set 0700 permissions on config directory %q: %w", filepath.Join(baseDir, subName), err)
		}
	}

	fileFD, err := ignoringEINTRInt(func() (int, error) {
		return unix.Openat(subFD, configFileName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0444)
	})
	if err != nil {
		_ = unix.Close(subFD)
		cleanupSubDir()
		return "", fmt.Errorf("failed to create config file in %q: %w", filepath.Join(baseDir, subName), err)
	}

	fullPath := filepath.Join(baseDir, subName, configFileName)
	f := os.NewFile(uintptr(fileFD), fullPath)
	cleanupAll := func() {
		_ = ignoringEINTR(func() error {
			return unix.Unlinkat(subFD, configFileName, 0)
		})
		_ = unix.Close(subFD)
		cleanupSubDir()
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanupAll()
		return "", fmt.Errorf("failed to write config file %q: %w", fullPath, err)
	}
	// Explicitly set 0444 regardless of host umask so non-root processes inside
	// the container can read the bind-mounted file inode, while the 0700 parent
	// subdirectory blocks access by other host UIDs.
	if err := f.Chmod(0444); err != nil {
		_ = f.Close()
		cleanupAll()
		return "", fmt.Errorf("failed to chmod config file %q: %w", fullPath, err)
	}
	if err := f.Close(); err != nil {
		cleanupAll()
		return "", fmt.Errorf("failed to close config file %q: %w", fullPath, err)
	}
	_ = unix.Close(subFD)
	return fullPath, nil
}

// RemoveConfigFile safely removes a startup config file (and its per-node
// subdirectory when using the 2-part layout) created under baseDir, refusing
// symlinks, hardlinks, and paths outside baseDir.
func RemoveConfigFile(baseDir, targetPath string) error {
	rel, err := relativeConfigPath(baseDir, targetPath)
	if err != nil {
		return err
	}

	euid := uint32(os.Geteuid())
	dirFD, err := openVerifiedDir(baseDir, false)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer func() { _ = unix.Close(dirFD) }()

	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 2 && parts[1] == configFileName && strings.HasPrefix(parts[0], "kne-"):
		subDir := parts[0]
		subFD, err := ignoringEINTRInt(func() (int, error) {
			return unix.Openat(dirFD, subDir, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		})
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return fmt.Errorf("failed to open config directory %q: %w", filepath.Join(baseDir, subDir), err)
		}

		var st unix.Stat_t
		if err := ignoringEINTR(func() error { return unix.Fstat(subFD, &st) }); err != nil {
			_ = unix.Close(subFD)
			return fmt.Errorf("failed to fstat config directory %q: %w", filepath.Join(baseDir, subDir), err)
		}
		if (st.Uid != euid && euid != 0) || (uint32(st.Mode)&07777) != 0700 {
			_ = unix.Close(subFD)
			return fmt.Errorf("refusing to remove config directory %q with unexpected owner (uid=%d) or mode (%#o)", filepath.Join(baseDir, subDir), st.Uid, uint32(st.Mode)&07777)
		}

		var fst unix.Stat_t
		err = ignoringEINTR(func() error {
			return unix.Fstatat(subFD, configFileName, &fst, unix.AT_SYMLINK_NOFOLLOW)
		})
		if err == nil {
			if (uint32(fst.Mode) & unix.S_IFMT) != unix.S_IFREG {
				_ = unix.Close(subFD)
				return fmt.Errorf("refusing to remove non-regular config file %q", targetPath)
			}
			if fst.Nlink != 1 {
				_ = unix.Close(subFD)
				return fmt.Errorf("refusing to remove hardlinked config file %q (nlink=%d)", targetPath, fst.Nlink)
			}
			if fst.Uid != euid && euid != 0 {
				_ = unix.Close(subFD)
				return fmt.Errorf("refusing to remove config file %q owned by uid %d", targetPath, fst.Uid)
			}
			if err := ignoringEINTR(func() error {
				return unix.Unlinkat(subFD, configFileName, 0)
			}); err != nil && !errors.Is(err, unix.ENOENT) {
				_ = unix.Close(subFD)
				return fmt.Errorf("failed to unlink config file %q: %w", targetPath, err)
			}
		} else if !errors.Is(err, unix.ENOENT) {
			_ = unix.Close(subFD)
			return fmt.Errorf("failed to stat config file %q: %w", targetPath, err)
		}

		_ = unix.Close(subFD)
		if err := ignoringEINTR(func() error {
			return unix.Unlinkat(dirFD, subDir, unix.AT_REMOVEDIR)
		}); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("failed to remove config directory %q: %w", filepath.Join(baseDir, subDir), err)
		}
		return nil

	case len(parts) == 1 && strings.HasPrefix(parts[0], "kne-") && strings.HasSuffix(parts[0], ".cfg"):
		fileName := parts[0]
		var fst unix.Stat_t
		err := ignoringEINTR(func() error {
			return unix.Fstatat(dirFD, fileName, &fst, unix.AT_SYMLINK_NOFOLLOW)
		})
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return fmt.Errorf("failed to stat legacy config file %q: %w", targetPath, err)
		}
		if (uint32(fst.Mode) & unix.S_IFMT) != unix.S_IFREG {
			return fmt.Errorf("refusing to remove non-regular legacy config file %q", targetPath)
		}
		if fst.Nlink != 1 {
			return fmt.Errorf("refusing to remove hardlinked legacy config file %q (nlink=%d)", targetPath, fst.Nlink)
		}
		if fst.Uid != euid && euid != 0 {
			return fmt.Errorf("refusing to remove legacy config file %q owned by uid %d", targetPath, fst.Uid)
		}
		if err := ignoringEINTR(func() error {
			return unix.Unlinkat(dirFD, fileName, 0)
		}); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("failed to unlink legacy config file %q: %w", targetPath, err)
		}
		return nil

	default:
		return fmt.Errorf("refusing to remove unexpected config path %q under %q", targetPath, baseDir)
	}
}
