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
	"fmt"
	"io"
	"os"
	"os/exec"
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
