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
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestEnsureDirCreatesAndTightensPermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("non-root 0700 permission test skipped when running as root")
	}

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "kne")

	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir(%q) failed: %v", dir, err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatalf("Lstat(%q) failed: %v", dir, err)
	}
	if got := fi.Mode().Perm(); got != 0700 {
		t.Errorf("EnsureDir(%q) perm = %#o, want 0700", dir, got)
	}

	// Simulate upgrade from an older KNE binary that created /tmp/kne with 0777.
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("Chmod(%q, 0777) failed: %v", dir, err)
	}
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir(%q) on 0777 dir failed: %v", dir, err)
	}
	fi, err = os.Lstat(dir)
	if err != nil {
		t.Fatalf("Lstat(%q) failed: %v", dir, err)
	}
	if got := fi.Mode().Perm(); got != 0700 {
		t.Errorf("EnsureDir(%q) tightened perm = %#o, want 0700", dir, got)
	}
}

func TestCheckHostDir(t *testing.T) {
	const dir = "/tmp/kne"
	tests := []struct {
		desc    string
		st      unix.Stat_t
		euid    uint32
		wantFix dirFix
		wantErr string
	}{{
		desc: "own private dir",
		st:   unix.Stat_t{Uid: 1000, Mode: unix.S_IFDIR | 0700},
		euid: 1000,
	}, {
		desc:    "own 0755 dir from an older KNE is tightened",
		st:      unix.Stat_t{Uid: 1000, Mode: unix.S_IFDIR | 0755},
		euid:    1000,
		wantFix: fixPrivate,
	}, {
		desc:    "own world-writable dir is tightened",
		st:      unix.Stat_t{Uid: 1000, Mode: unix.S_IFDIR | 0777},
		euid:    1000,
		wantFix: fixPrivate,
	}, {
		desc: "root sticky dir from tmpfiles.d used by non-root",
		st:   unix.Stat_t{Uid: 0, Mode: unix.S_IFDIR | unix.S_ISVTX | 0777},
		euid: 1000,
	}, {
		desc: "root 0755 dir created by docker used by non-root",
		st:   unix.Stat_t{Uid: 0, Mode: unix.S_IFDIR | 0755},
		euid: 1000,
	}, {
		desc:    "root world-writable dir without sticky bit",
		st:      unix.Stat_t{Uid: 0, Mode: unix.S_IFDIR | 0777},
		euid:    1000,
		wantErr: "without the sticky bit",
	}, {
		desc:    "root group-writable dir without sticky bit",
		st:      unix.Stat_t{Uid: 0, Mode: unix.S_IFDIR | 0775},
		euid:    1000,
		wantErr: "without the sticky bit",
	}, {
		desc:    "root 0755 dir used by root is made sticky",
		st:      unix.Stat_t{Uid: 0, Mode: unix.S_IFDIR | 0755},
		euid:    0,
		wantFix: fixSticky,
	}, {
		desc: "root sticky dir used by root",
		st:   unix.Stat_t{Uid: 0, Mode: unix.S_IFDIR | unix.S_ISVTX | 0777},
		euid: 0,
	}, {
		desc:    "private dir owned by another user",
		st:      unix.Stat_t{Uid: 1001, Mode: unix.S_IFDIR | 0700},
		euid:    1000,
		wantErr: "owned by uid 1001",
	}, {
		desc:    "sticky dir owned by another user",
		st:      unix.Stat_t{Uid: 1001, Mode: unix.S_IFDIR | unix.S_ISVTX | 0777},
		euid:    1000,
		wantErr: "owned by uid 1001",
	}, {
		desc:    "user-owned dir used by root",
		st:      unix.Stat_t{Uid: 1000, Mode: unix.S_IFDIR | 0700},
		euid:    0,
		wantErr: "owned by uid 1000",
	}, {
		desc:    "regular file",
		st:      unix.Stat_t{Uid: 1000, Mode: unix.S_IFREG | 0600},
		euid:    1000,
		wantErr: "not a directory",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			gotFix, err := checkHostDir(dir, &tt.st, tt.euid)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("checkHostDir() error = %v, want error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("checkHostDir() unexpected error: %v", err)
			}
			if gotFix != tt.wantFix {
				t.Errorf("checkHostDir() fix = %d, want %d", gotFix, tt.wantFix)
			}
		})
	}
}

func TestEnsureDirRejectsSymlinkAndFileSquat(t *testing.T) {
	tmp := t.TempDir()

	target := filepath.Join(tmp, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	symDir := filepath.Join(tmp, "kne-sym")
	if err := os.Symlink(target, symDir); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(symDir); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("EnsureDir(symlink) = %v, want symlink error", err)
	}

	fileSquat := filepath.Join(tmp, "kne-file")
	if err := os.WriteFile(fileSquat, []byte("squat"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(fileSquat); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("EnsureDir(regular file) = %v, want not-a-directory error", err)
	}
}

func TestEnsureDirParentValidation(t *testing.T) {
	tmp := t.TempDir()

	// World-writable parent without sticky bit must be rejected.
	unsafeParent := filepath.Join(tmp, "world-writable")
	if err := os.Mkdir(unsafeParent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafeParent, 0777); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(filepath.Join(unsafeParent, "kne")); err == nil || !strings.Contains(err.Error(), "world-writable without sticky bit") {
		t.Errorf("EnsureDir under non-sticky 0777 parent = %v, want world-writable error", err)
	}

	// Trusted same-UID parent symlink (simulating macOS /tmp -> private/tmp) must work
	// and preserve the unexpanded baseDir prefix in the returned HostPath.
	realParent := filepath.Join(tmp, "private-tmp")
	if err := os.Mkdir(realParent, 0700); err != nil {
		t.Fatal(err)
	}
	symParent := filepath.Join(tmp, "mac-tmp")
	if err := os.Symlink(realParent, symParent); err != nil {
		t.Fatal(err)
	}
	baseDir := filepath.Join(symParent, "kne")
	gotPath, err := CreateConfigFile(baseDir, "r1", []byte("hostname r1\n"))
	if err != nil {
		t.Fatalf("CreateConfigFile under trusted symlink parent failed: %v", err)
	}
	if !strings.HasPrefix(gotPath, baseDir+"/") {
		t.Errorf("CreateConfigFile path = %q, want prefix %q", gotPath, baseDir+"/")
	}
}

func TestCreateConfigFilePermissionsUmaskAndSanitization(t *testing.T) {
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, "kne")

	oldUmask := unix.Umask(0077)
	defer unix.Umask(oldUmask)

	payload := bytes.Repeat([]byte("interface Ethernet1\n"), 110000) // ~2.2 MB
	gotPath, err := CreateConfigFile(baseDir, "../r1/evil:node", payload)
	if err != nil {
		t.Fatalf("CreateConfigFile() failed: %v", err)
	}

	wantRe := regexp.MustCompile(`^` + regexp.QuoteMeta(baseDir) + `/kne-\.\._r1_evil_node-config-[0-9a-f]{16}/config\.cfg$`)
	if !wantRe.MatchString(gotPath) {
		t.Errorf("CreateConfigFile() path = %q, did not match %s", gotPath, wantRe)
	}

	subFi, err := os.Lstat(filepath.Dir(gotPath))
	if err != nil {
		t.Fatalf("Lstat(subDir) failed: %v", err)
	}
	if got := subFi.Mode().Perm(); got != 0700 {
		t.Errorf("subDir perm = %#o, want 0700", got)
	}

	fileFi, err := os.Lstat(gotPath)
	if err != nil {
		t.Fatalf("Lstat(configFile) failed: %v", err)
	}
	if got := fileFi.Mode().Perm(); got != 0444 {
		t.Errorf("configFile perm under umask 0077 = %#o, want 0444", got)
	}

	gotBytes, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) failed: %v", gotPath, err)
	}
	if !bytes.Equal(gotBytes, payload) {
		t.Errorf("configFile byte length = %d, want %d (exact payload match)", len(gotBytes), len(payload))
	}
}

func TestRemoveConfigFile(t *testing.T) {
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, "kne")

	// 1. Standard 2-part layout cleanup removes both config.cfg and subDir.
	cfgPath, err := CreateConfigFile(baseDir, "r1", []byte("cfg1"))
	if err != nil {
		t.Fatalf("CreateConfigFile() failed: %v", err)
	}
	subDir := filepath.Dir(cfgPath)
	if err := RemoveConfigFile(baseDir, cfgPath); err != nil {
		t.Fatalf("RemoveConfigFile(%q) failed: %v", cfgPath, err)
	}
	if _, err := os.Lstat(subDir); !os.IsNotExist(err) {
		t.Errorf("expected subDir %q to be removed, got err=%v", subDir, err)
	}

	// 2. Partial state where config.cfg is already missing still removes subDir.
	cfgPath2, err := CreateConfigFile(baseDir, "r2", []byte("cfg2"))
	if err != nil {
		t.Fatalf("CreateConfigFile() failed: %v", err)
	}
	if err := os.Remove(cfgPath2); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfigFile(baseDir, cfgPath2); err != nil {
		t.Fatalf("RemoveConfigFile(missing config.cfg) failed: %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(cfgPath2)); !os.IsNotExist(err) {
		t.Errorf("expected empty subDir %q to be removed, got err=%v", filepath.Dir(cfgPath2), err)
	}

	// 3. Legacy 1-part file layout cleanup.
	legacyPath := filepath.Join(baseDir, "kne-r3-config-12345.cfg")
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfigFile(baseDir, legacyPath); err != nil {
		t.Fatalf("RemoveConfigFile(legacy) failed: %v", err)
	}
	if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("expected legacy file %q to be removed, got err=%v", legacyPath, err)
	}

	// 4. Hardlinked file (Nlink > 1) must be refused.
	cfgPathHL, err := CreateConfigFile(baseDir, "r4", []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(tmp, "hardlink.cfg")
	if err := os.Link(cfgPathHL, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfigFile(baseDir, cfgPathHL); err == nil || !strings.Contains(err.Error(), "hardlinked") {
		t.Errorf("RemoveConfigFile(hardlink) = %v, want hardlinked error", err)
	}

	// 5. Symlinked config.cfg must be refused without deleting target.
	victim := filepath.Join(tmp, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPathSym, err := CreateConfigFile(baseDir, "r5", []byte("orig"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(cfgPathSym); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, cfgPathSym); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfigFile(baseDir, cfgPathSym); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Errorf("RemoveConfigFile(symlink) = %v, want non-regular error", err)
	}
	if _, err := os.Lstat(victim); err != nil {
		t.Errorf("victim file %q was affected: %v", victim, err)
	}

	// 6. Path outside baseDir must be refused without deleting victim.
	if err := RemoveConfigFile(baseDir, victim); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("RemoveConfigFile(outside) = %v, want outside error", err)
	}
	if _, err := os.Lstat(victim); err != nil {
		t.Errorf("victim file %q was deleted: %v", victim, err)
	}

	// 7. Symlinked subDir must be refused without deleting target.
	victimDir := filepath.Join(tmp, "victim-dir")
	if err := os.Mkdir(victimDir, 0700); err != nil {
		t.Fatal(err)
	}
	victimDirFile := filepath.Join(victimDir, configFileName)
	if err := os.WriteFile(victimDirFile, []byte("keep dir file"), 0600); err != nil {
		t.Fatal(err)
	}
	symSubDir := filepath.Join(baseDir, "kne-r6-config-deadbeef")
	if err := os.Symlink(victimDir, symSubDir); err != nil {
		t.Fatal(err)
	}
	if err := RemoveConfigFile(baseDir, filepath.Join(symSubDir, configFileName)); err == nil {
		t.Errorf("RemoveConfigFile(symlink subDir) = nil, want error")
	}
	if _, err := os.Lstat(victimDirFile); err != nil {
		t.Errorf("victim file inside symlinked subDir %q was affected: %v", victimDirFile, err)
	}

	// 8. Missing baseDir or missing subDir is idempotent.
	if err := RemoveConfigFile(filepath.Join(tmp, "nonexistent"), filepath.Join(tmp, "nonexistent", "kne-r1-config-abc", "config.cfg")); err != nil {
		t.Errorf("RemoveConfigFile(missing baseDir) = %v, want nil", err)
	}
}
