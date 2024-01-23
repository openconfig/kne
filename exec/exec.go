// Package exec wraps the os/exec package.  It facilitates testing by use of the sub-packge fake.
//
// Example
//
//	var buf bytes.Buffer
//	c := Command("echo", "bob")
//	c.SetStdout(&buf)
//	err := c.Run()
package exec

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/openconfig/kne/logshim"
	log "k8s.io/klog/v2"
)

// A Cmd is an interface representing a command to run.
type Cmd interface {
	SetStdout(io.Writer) // Redirect standard output to the writer
	SetStderr(io.Writer) // Redirect standard error to the writer
	SetStdin(io.Reader)  // Redirect standard in to the reader
	Run() error          // Operates the same as os/exec/Cmd.Run
}

// Command is a variable that normally points to NewCommand.
// It is a variable so tests can redirect calls to a fake.
var Command func(cmd string, args ...string) Cmd = NewCommand

// NewCommand returns a Cmd that can run the supplied command.
// NewCommand sets up stdout and stderr to go to os.Stdout and os.Stderr.
// SetStdout and SetStderr are used to change where to send the output.
//
// Most programs should use exec.Command rather than exec.NewCommand.
func NewCommand(cmd string, args ...string) Cmd {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return command{cmd: c}
}

// A command wraps the standard os/exec Cmd type.  Since it only contains one
// pointer we can pass it by value.
type command struct {
	cmd *exec.Cmd
}

func (c command) SetStdout(w io.Writer) { c.cmd.Stdout = w }

func (c command) SetStderr(w io.Writer) { c.cmd.Stderr = w }

func (c command) SetStdin(r io.Reader) { c.cmd.Stdin = r }

func (c command) Run() error {
	if err := c.cmd.Run(); err != nil {
		return fmt.Errorf("%q failed: %v", c.cmd.String(), err)
	}
	return nil
}

// runCommand is a wrapper utility function that creates and runs a command with
// various inputs and settings.
func runCommand(writeLogs bool, in []byte, cmd string, args ...string) ([]byte, error) {
	c := Command(cmd, args...)
	var out bytes.Buffer
	c.SetStdout(&out)
	c.SetStderr(&out)
	if writeLogs {
		outLog := logshim.New(func(v ...interface{}) {
			log.Info(append([]interface{}{"(" + cmd + "): "}, v...)...)
		})
		errLog := logshim.New(func(v ...interface{}) {
			log.Warning(append([]interface{}{"(" + cmd + "): "}, v...)...)
		})
		defer func() {
			outLog.Close()
			errLog.Close()
		}()
		c.SetStdout(io.MultiWriter(outLog, &out))
		c.SetStderr(io.MultiWriter(errLog, &out))
	}
	if len(in) > 0 {
		c.SetStdin(bytes.NewReader(in))
	}
	err := c.Run()
	return out.Bytes(), err
}

// LogCommand runs the specified command but records standard output
// with log.Info and standard error with log.Warning.
func LogCommand(cmd string, args ...string) error {
	_, err := runCommand(true, nil, cmd, args...)
	return err
}

// LogCommandWithInput runs the specified command but records standard output
// with log.Info and standard error with log.Warning. in is sent to
// the standard input of the command.
func LogCommandWithInput(in []byte, cmd string, args ...string) error {
	_, err := runCommand(true, in, cmd, args...)
	return err
}

// OutLogCommand runs the specified command but records standard output
// with log.Info and standard error with log.Warning. Standard output
// and standard error are also returned.
func OutLogCommand(cmd string, args ...string) ([]byte, error) {
	return runCommand(true, nil, cmd, args...)
}

// OutCommand runs the specified command and returns any standard output
// as well as any errors.
func OutCommand(cmd string, args ...string) ([]byte, error) {
	return runCommand(false, nil, cmd, args...)
}
