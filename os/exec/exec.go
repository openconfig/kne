// Copyright 2021 Google LLC
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

type Response struct {
	Err    error
	Stdout string
	Stderr string
}

type FakeExecer struct {
	*Execer
	responses []Response
}

func NewFakeExecer(errs ...error) *FakeExecer {
	var resp []Response
	for _, e := range errs {
		resp = append(resp, Response{Err: e})
	}
	return &FakeExecer{
		Execer:    NewExecer(nil, nil),
		responses: resp,
	}
}

func NewFakeExecerWithIO(stdout, stderr io.Writer, resp ...Response) *FakeExecer {
	return &FakeExecer{
		Execer:    NewExecer(stdout, stderr),
		responses: resp,
	}
}
func (f *FakeExecer) Exec(cmd string, _ ...string) error {
	if len(f.responses) == 0 {
		return fmt.Errorf("unexpected exec(%q) call", cmd)
	}
	r := f.responses[0]
	f.responses = f.responses[1:]
	if r.Err != nil {
		return r.Err
	}
	if r.Stdout != "" {
		fmt.Fprintf(f.stdout, r.Stdout)
	}
	if r.Stderr != "" {
		fmt.Fprintf(f.stderr, r.Stderr)
	}
	return r.Err
}
