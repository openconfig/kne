// Copyright 2021 Google LLC
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
// limitations under the License.package exec
package exec

import (
	"fmt"
	"io"
	"os/exec"
)

type Execer struct {
	stdout io.Writer
	stderr io.Writer
}

func NewExecer(stdout, stderr io.Writer) *Execer {
	return &Execer{stdout: stdout, stderr: stderr}
}

func (e *Execer) Exec(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = e.stdout
	c.Stderr = e.stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%q failed: %v", c.String(), err)
	}
	return nil
}

func (e *Execer) SetStdout(stdout io.Writer) {
	e.stdout = stdout
}

func (e *Execer) SetStderr(stderr io.Writer) {
	e.stderr = stderr
}

type FakeExecer struct {
	execErrs []error
}

func NewFakeExecer(execErrs ...error) *FakeExecer {
	return &FakeExecer{execErrs: execErrs}
}

func (f *FakeExecer) Exec(cmd string, _ ...string) error {
	switch len(f.execErrs) {
	default:
		err := f.execErrs[0]
		f.execErrs = f.execErrs[1:]
		return err
	case 0:
		return fmt.Errorf("unexpected exec(%q) call", cmd)
	case 1:
		err := f.execErrs[0]
		f.execErrs = []error{}
		return err
	}
}

func (f *fakeExecer) SetStdout(stdout io.Writer) {}

func (f *fakeExecer) SetStderr(stderr io.Writer) {}
