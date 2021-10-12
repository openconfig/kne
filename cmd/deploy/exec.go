package deploy

import (
	"fmt"
	"io"
	"os/exec"
	"errors"

	log "github.com/sirupsen/logrus"
)

type execer struct {
	stdout io.Writer
	stderr io.Writer
}

func newExecer(stdout, stderr io.Writer) *execer {
	return &execer{stdout: stdout, stderr: stderr}
}

func (e *execer) exec(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = e.stdout
	c.Stderr = e.stderr
	log.Info(c.String())
	if err := c.Run(); err != nil {
		return fmt.Errorf("%q failed: %v", c.String(), err)
	}
	return nil
}

func (e *execer) setStdout(stdout io.Writer) {
	e.stdout = stdout
}

func (e *execer) setStderr(stderr io.Writer) {
	e.stderr = stderr
}

type fakeExecer struct {
	execErrs []error
}

func newFakeExecer(execErrs ...error) *fakeExecer {
	return &fakeExecer{execErrs: execErrs}
}

func (f *fakeExecer) exec(_ string, _ ...string) error {
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

func (f *fakeExecer) setStdout(stdout io.Writer) {}

func (f *fakeExecer) setStderr(stderr io.Writer) {}
