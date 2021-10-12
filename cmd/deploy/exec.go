package exec

import (
	"fmt"
	"io"
	"os/exec"

	log "github.com/sirupsen/logrus"
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
	log.Info(c.String())
	if err := c.Run(); err != nil {
		return fmt.Errorf("%q failed: %v", c.String(), err)
	}
	return nil
}

func (e *Execer) SetStdout(stdout io.Writer) {
	e.Stdout = stdout
}

func (e *Execer) SetStderr(stderr io.Writer) {
	e.Stderr = stderr
}

type FakeExecer struct {
	execErrs []error
}

func NewFakeExecer(execErrs ...error) *FakeExecer {
	return &FakeExecer{execErrs: ...execErrs}
}

func (f *FakeExecer) Exec(_ string, _ ...string) error {
	switch len(f.execErrs) {
	default:
		err := f.execErrs[0]
		f.execErrs = f.execErrs[1:]
		return err
	case 0:
		return errors.New("unexpected Exec() call")
	case 1:
		err := f.execErrs[0]
		f.execErrs = []error{}
		return err
	}
}

func (f *FakeExecer) SetStdout(stdout io.Writer) {}

func (f *FakeExecer) SetStderr(stderr io.Writer) {}
