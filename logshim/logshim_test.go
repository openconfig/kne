// Copyright 2023 Google LLC
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

package logshim

import (
	"bytes"
	"fmt"
	"testing"
)

func TestShim(t *testing.T) {
	var b bytes.Buffer

	s := New(func(args ...interface{}) {
		b.Write([]byte("Prefix:"))
		fmt.Fprintln(&b, args...)
	})
	want1 := `Prefix:Line 1
Prefix:Line 2
Prefix:Line 3
`
	want2 := want1 + `Prefix:partial line 1
`
	want3 := want2 + `Prefix:partial line 2
`
	// Writing with a partial line.
	s.Write([]byte(`Line 1
Line 2
Line 3
partial line 1`))
	if got := b.String(); got != want1 {
		t.Errorf("First: Got %q, want %q", got, want1)
	}

	// Force the timer to go off
	testChannel = make(chan struct{})
	s.timer.Reset(1)
	<-testChannel
	if got := b.String(); got != want2 {
		t.Errorf("First: Got %q, want %q", got, want2)
	}

	// Write another partial line and close the shim
	s.Write([]byte(`partial line 2`))
	s.Close()
	if got := b.String(); got != want3 {
		t.Errorf("Second: Got %q, want %q", got, want3)
	}
}
