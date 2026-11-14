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

// Package hostdir manages KNE's host-mounted temporary configuration directory
// safely on single-user and multi-user hosts without following symlinks.
package hostdir

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// DefaultDir is the default host directory bind-mounted into KinD nodes
	// and used for >=1MB node startup configuration HostPath volumes.
	DefaultDir = "/tmp/kne"

	configFileName      = "config.cfg"
	maxSanitizedNodeLen = 64
)

func sanitizeNodeName(name string) string {
	var b strings.Builder
	b.Grow(min(len(name), maxSanitizedNodeLen))
	for i := 0; i < len(name) && b.Len() < maxSanitizedNodeLen; i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if s == "" || s == "." || s == ".." {
		return "node"
	}
	return s
}

func validateCleanAbsDir(dir string) error {
	if !filepath.IsAbs(dir) || dir != filepath.Clean(dir) {
		return fmt.Errorf("host directory path %q must be clean and absolute", dir)
	}
	if filepath.Dir(dir) == dir {
		return fmt.Errorf("host directory path %q must not be filesystem root", dir)
	}
	base := filepath.Base(dir)
	if base == "." || base == ".." || base == "/" {
		return fmt.Errorf("invalid host directory basename %q in %q", base, dir)
	}
	return nil
}

func relativeConfigPath(baseDir, targetPath string) (string, error) {
	if err := validateCleanAbsDir(baseDir); err != nil {
		return "", err
	}
	if !filepath.IsAbs(targetPath) || targetPath != filepath.Clean(targetPath) {
		return "", fmt.Errorf("config file path %q must be clean and absolute", targetPath)
	}
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("refusing to operate on path %q outside %q", targetPath, baseDir)
	}
	return filepath.ToSlash(rel), nil
}
