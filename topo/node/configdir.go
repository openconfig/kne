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

package node

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultConfigDir is the host directory that KinD bind-mounts into its nodes
// and that holds startup configs too large for a ConfigMap.
const DefaultConfigDir = "/tmp/kne"

const configFileName = "config.cfg"

// EnsureConfigDir creates dir if needed and verifies that no other non-root
// user can add, replace or remove its entries: dir must be a real directory
// (not a symlink) owned by the effective UID, or owned by root and not
// writable by others unless sticky. A directory owned by the effective UID is
// tightened to 0700.
//
// Path-based checks are sufficient because dir lives in a sticky, root-owned
// directory (/tmp): once dir is owned by us or by root, no other user can
// rename or replace it.
func EnsureConfigDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return checkConfigDir(dir)
}

func checkConfigDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s must be a directory, not a symlink or file", dir)
	}
	uid, ok := fileOwner(fi)
	if !ok {
		return nil
	}
	euid := os.Geteuid()
	mode := fi.Mode()
	repair := fmt.Sprintf("repair with: sudo chown root:root %s && sudo chmod 1777 %s", dir, dir)
	switch {
	case uid == euid && euid != 0:
		if mode.Perm() != 0o700 || mode&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
			return os.Chmod(dir, 0o700)
		}
	case uid == 0:
		if mode.Perm()&0o022 != 0 && mode&fs.ModeSticky == 0 {
			return fmt.Errorf("%s is writable by other users without the sticky bit; %s", dir, repair)
		}
	default:
		return fmt.Errorf("%s is owned by another user (uid %d); %s", dir, uid, repair)
	}
	return nil
}

// writeConfigFile writes data to a 0444 config file in a new, randomly named
// 0700 subdirectory of dir and returns its path.
func writeConfigFile(dir, nodeName string, data []byte) (string, error) {
	if err := EnsureConfigDir(dir); err != nil {
		return "", err
	}
	subDir, err := os.MkdirTemp(dir, fmt.Sprintf("kne-%s-config-*", nodeName))
	if err != nil {
		return "", err
	}
	path := filepath.Join(subDir, configFileName)
	if err := writeReadOnly(path, data); err != nil {
		return "", errors.Join(err, os.RemoveAll(subDir))
	}
	return path, nil
}

func writeReadOnly(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o444)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	// Set the mode explicitly so the umask can't hide the file from
	// non-root processes in the node container.
	if err := f.Chmod(0o444); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// removeConfigFile removes a config file written by writeConfigFile (or a
// flat kne-*.cfg file written by older KNE versions) and refuses any path
// outside dir.
func removeConfigFile(dir, path string) error {
	rel, err := filepath.Rel(dir, path)
	parts := strings.Split(filepath.ToSlash(rel), "/")
	switch {
	case err != nil || !filepath.IsLocal(rel) || !strings.HasPrefix(parts[0], "kne-"):
	case len(parts) == 1 && strings.HasSuffix(parts[0], ".cfg"): // Older KNE versions.
		return removeIn(dir, path, "")
	case len(parts) == 2 && parts[1] == configFileName:
		return removeIn(dir, path, filepath.Join(dir, parts[0]))
	}
	return fmt.Errorf("refusing to remove %s: not a KNE config file in %s", path, dir)
}

func removeIn(dir, path, subDir string) error {
	if err := checkConfigDir(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if subDir != "" {
		fi, err := os.Lstat(subDir)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("refusing to remove %s: %s is not a directory", path, subDir)
		}
	}
	// os.Remove never follows a symlink in the final path element.
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if subDir != "" {
		if err := os.Remove(subDir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
