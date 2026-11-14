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

//go:build !unix

package hostdir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnsureDir ensures dir exists as a non-symlink directory with 0700 permissions.
func EnsureDir(dir string) error {
	if err := validateCleanAbsDir(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("host path %q must be a real directory", dir)
	}
	return os.Chmod(dir, 0700)
}

// CreateConfigFile writes data into a newly created 0444 file inside a 0700
// subdirectory under baseDir.
func CreateConfigFile(baseDir, nodeName string, data []byte) (string, error) {
	if err := EnsureDir(baseDir); err != nil {
		return "", err
	}
	safeNode := sanitizeNodeName(nodeName)
	subDir, err := os.MkdirTemp(baseDir, fmt.Sprintf("kne-%s-config-*", safeNode))
	if err != nil {
		return "", err
	}
	fullPath := filepath.Join(subDir, configFileName)
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0444)
	if err != nil {
		_ = os.Remove(subDir)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.RemoveAll(subDir)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.RemoveAll(subDir)
		return "", err
	}
	return fullPath, nil
}

// RemoveConfigFile removes a startup config file created under baseDir.
func RemoveConfigFile(baseDir, targetPath string) error {
	rel, err := relativeConfigPath(baseDir, targetPath)
	if err != nil {
		return err
	}
	parts := strings.Split(rel, "/")
	switch {
	case len(parts) == 2 && parts[1] == configFileName && strings.HasPrefix(parts[0], "kne-"):
		subPath := filepath.Join(baseDir, parts[0])
		sfi, err := os.Lstat(subPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if sfi.Mode()&os.ModeSymlink != 0 || !sfi.IsDir() {
			return fmt.Errorf("refusing to remove non-directory %q", subPath)
		}
		ffi, err := os.Lstat(targetPath)
		if err == nil {
			if !ffi.Mode().IsRegular() {
				return fmt.Errorf("refusing to remove non-regular file %q", targetPath)
			}
			if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(subPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	case len(parts) == 1 && strings.HasPrefix(parts[0], "kne-") && strings.HasSuffix(parts[0], ".cfg"):
		ffi, err := os.Lstat(targetPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !ffi.Mode().IsRegular() {
			return fmt.Errorf("refusing to remove non-regular legacy file %q", targetPath)
		}
		if err := os.Remove(targetPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("refusing to remove unexpected config path %q under %q", targetPath, baseDir)
	}
}
