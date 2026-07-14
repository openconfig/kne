// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package release

import (
	"testing"
)

func TestNewRelease(t *testing.T) {
	cmd := NewRelease()
	if cmd == nil {
		t.Fatalf("NewRelease() returned nil")
	}
	if cmd.Use != "release" {
		t.Errorf("cmd.Use = %q, want %q", cmd.Use, "release")
	}
	meshnetCmd, _, err := cmd.Find([]string{"meshnet"})
	if err != nil {
		t.Fatalf("cmd.Find([\"meshnet\"]) failed: %v", err)
	}
	if meshnetCmd == nil {
		t.Fatalf("meshnet subcommand not found")
	}
}
