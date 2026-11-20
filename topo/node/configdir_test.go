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

package node

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	topopb "github.com/openconfig/kne/proto/topo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kfake "k8s.io/client-go/kubernetes/fake"
)

func TestEnsureConfigDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("ownership checks behave differently as root")
	}
	tests := []struct {
		desc     string
		setup    func(t *testing.T, dir string)
		wantErr  string
		wantPerm os.FileMode
	}{{
		desc:     "creates missing dir as 0700",
		wantPerm: 0o700,
	}, {
		desc: "tightens own world-writable dir",
		setup: func(t *testing.T, dir string) {
			mustMkdir(t, dir, 0o777)
		},
		wantPerm: 0o700,
	}, {
		desc: "rejects symlink",
		setup: func(t *testing.T, dir string) {
			target := filepath.Join(filepath.Dir(dir), "target")
			mustMkdir(t, target, 0o700)
			if err := os.Symlink(target, dir); err != nil {
				t.Fatal(err)
			}
		},
		wantErr: "not a symlink or file",
	}, {
		desc: "rejects file",
		setup: func(t *testing.T, dir string) {
			if err := os.WriteFile(dir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		wantErr: "not a symlink or file",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "kne")
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			err := EnsureConfigDir(dir)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("EnsureConfigDir() unexpected error: %s", s)
			}
			if tt.wantErr != "" {
				return
			}
			fi, err := os.Lstat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode() & os.ModePerm; got != tt.wantPerm {
				t.Errorf("EnsureConfigDir() perm = %#o, want %#o", got, tt.wantPerm)
			}
		})
	}
}

func TestCheckConfigDirOwnership(t *testing.T) {
	euid := os.Geteuid()
	if euid == 0 {
		t.Skip("ownership checks behave differently as root")
	}
	// Root-owned directories that exist on any Linux host.
	for _, tt := range []struct {
		dir     string
		wantErr string
	}{
		{dir: "/tmp"},     // root, 1777: sticky.
		{dir: "/usr/bin"}, // root, 0755: not writable by others.
	} {
		fi, err := os.Lstat(tt.dir)
		if err != nil || fi.Sys().(*syscall.Stat_t).Uid != 0 {
			t.Logf("skipping %s: not a root-owned directory on this host", tt.dir)
			continue
		}
		if err := checkConfigDir(tt.dir); err != nil {
			t.Errorf("checkConfigDir(%q) unexpected error: %v", tt.dir, err)
		}
	}
}

func TestWriteAndRemoveConfigFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kne")
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)

	data := bytes.Repeat([]byte("x"), 2<<20)
	path, err := writeConfigFile(dir, "r1", data)
	if err != nil {
		t.Fatalf("writeConfigFile() failed: %v", err)
	}
	sub := filepath.Dir(path)
	if filepath.Dir(sub) != dir || filepath.Base(path) != configFileName {
		t.Errorf("writeConfigFile() path = %q, want %s/kne-r1-config-*/%s", path, dir, configFileName)
	}
	for p, want := range map[string]os.FileMode{sub: 0o700, path: 0o444} {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode() & os.ModePerm; got != want {
			t.Errorf("perm of %s = %#o, want %#o", p, got, want)
		}
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Errorf("ReadFile(%q) = %d bytes, %v; want %d bytes", path, len(got), err, len(data))
	}

	if err := removeConfigFile(dir, path); err != nil {
		t.Fatalf("removeConfigFile() failed: %v", err)
	}
	if _, err := os.Lstat(sub); !os.IsNotExist(err) {
		t.Errorf("removeConfigFile() left %s behind: %v", sub, err)
	}
	if err := removeConfigFile(dir, path); err != nil {
		t.Errorf("removeConfigFile() of a removed file = %v, want nil", err)
	}
}

func TestRemoveConfigFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "kne")
	mustMkdir(t, dir, 0o700)
	victim := filepath.Join(tmp, "victim")
	mustMkdir(t, victim, 0o700)
	victimFile := filepath.Join(victim, configFileName)
	if err := os.WriteFile(victimFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "kne-r1-config-123.cfg")
	if err := os.WriteFile(legacy, []byte("legacy"), 0o444); err != nil {
		t.Fatal(err)
	}
	symSub := filepath.Join(dir, "kne-r2-config-456")
	if err := os.Symlink(victim, symSub); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		desc    string
		path    string
		wantErr string
	}{
		{desc: "legacy flat file", path: legacy},
		{desc: "outside dir", path: victimFile, wantErr: "refusing"},
		{desc: "escapes dir", path: filepath.Join(dir, "..", "victim", configFileName), wantErr: "refusing"},
		{desc: "not a KNE file", path: filepath.Join(dir, "other.cfg"), wantErr: "refusing"},
		{desc: "too deep", path: filepath.Join(dir, "kne-a", "b", configFileName), wantErr: "refusing"},
		{desc: "symlinked subdir", path: filepath.Join(symSub, configFileName), wantErr: "not a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if s := errdiff.Substring(removeConfigFile(dir, tt.path), tt.wantErr); s != "" {
				t.Errorf("removeConfigFile(%q) unexpected error: %s", tt.path, s)
			}
		})
	}
	if _, err := os.Lstat(legacy); !os.IsNotExist(err) {
		t.Errorf("legacy file %s not removed: %v", legacy, err)
	}
	if _, err := os.Lstat(victimFile); err != nil {
		t.Errorf("victim file %s was removed: %v", victimFile, err)
	}
	if err := removeConfigFile(filepath.Join(tmp, "missing"), filepath.Join(tmp, "missing", "kne-x", configFileName)); err != nil {
		t.Errorf("removeConfigFile() with missing dir = %v, want nil", err)
	}
}

func TestWriteConfigFilePermissionRepairHint(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to 0755 directories")
	}
	const dir = "/usr/bin"
	fi, err := os.Lstat(dir)
	if err != nil || fi.Sys().(*syscall.Stat_t).Uid != 0 || fi.Mode().Perm()&0o022 != 0 {
		t.Skipf("skipping: %s is not a root-owned read-only directory on this host", dir)
	}
	_, err = writeConfigFile(dir, "r1", []byte("cfg"))
	want := "repair with: sudo chown root:root /usr/bin && sudo chmod 1777 /usr/bin"
	if s := errdiff.Substring(err, want); s != "" {
		t.Fatalf("writeConfigFile(%q) unexpected error: %s", dir, s)
	}
}

func TestRemoveInSubDirOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kne")
	path, err := writeConfigFile(dir, "r1", []byte("keep"))
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	subName := filepath.Base(filepath.Dir(path))
	err = removeIn(dir, path, rel, subName, os.Geteuid()+1)
	want := fmt.Sprintf("owned by uid %d", os.Geteuid())
	if s := errdiff.Substring(err, want); s != "" {
		t.Fatalf("removeIn() unexpected error: %s", s)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("config file %s was affected: %v", path, err)
	}
}

func TestRemoveConfigFilePartialAndIntraDirSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kne")
	path, err := writeConfigFile(dir, "r1", []byte("cfg1"))
	if err != nil {
		t.Fatal(err)
	}
	// If config.cfg is already gone, removeConfigFile still removes the empty subdir.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := removeConfigFile(dir, path); err != nil {
		t.Fatalf("removeConfigFile() with missing config.cfg failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("removeConfigFile() left %s behind: %v", filepath.Dir(path), err)
	}

	// A relative symlink inside dir pointing to another subdir must be refused.
	keepPath, err := writeConfigFile(dir, "r2", []byte("keep"))
	if err != nil {
		t.Fatal(err)
	}
	symSub := filepath.Join(dir, "kne-r3-config-sym")
	if err := os.Symlink(filepath.Base(filepath.Dir(keepPath)), symSub); err != nil {
		t.Fatal(err)
	}
	if s := errdiff.Substring(removeConfigFile(dir, filepath.Join(symSub, configFileName)), "not a directory"); s != "" {
		t.Errorf("removeConfigFile(intra-dir symlink) unexpected error: %s", s)
	}
	if _, err := os.Lstat(keepPath); err != nil {
		t.Errorf("target file %s was affected: %v", keepPath, err)
	}
}

func TestDeleteConfigSrcVolume(t *testing.T) {
	ctx := context.Background()
	origTempCfgDir := tempCfgDir
	defer func() { tempCfgDir = origTempCfgDir }()
	tempCfgDir = filepath.Join(t.TempDir(), "kne")

	path, err := writeConfigFile(tempCfgDir, "dev1", []byte("config"))
	if err != nil {
		t.Fatal(err)
	}
	n := &Impl{
		Namespace: "test",
		Proto:     &topopb.Node{Name: "dev1"},
		KubeClient: kfake.NewSimpleClientset(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "dev1", Namespace: "test"},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name:         ConfigSrcVolumeName,
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: path}},
			}}},
		}),
	}
	if err := n.DeleteConfig(ctx); err != nil {
		t.Fatalf("DeleteConfig() failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Errorf("DeleteConfig() left %s behind: %v", filepath.Dir(path), err)
	}
}

func mustMkdir(t *testing.T, dir string, perm os.FileMode) {
	t.Helper()
	if err := os.Mkdir(dir, perm); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, perm); err != nil {
		t.Fatal(err)
	}
}
