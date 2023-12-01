// Package fake is used to fake calls github.com/openconfig/kne/exec.
//
// Typical Usage:
//
//	import "github.com/openconfig/kne/exec"
//
//	{
//		responses := []fake.Response{...}
//		cmds := fake.Commands(responses)
//		oCommand := kexec.Command
//		defer func() {
//			kexec.Command = oCommand
//			if err := cmds.Done(); err != nil {
//				// handle the error
//			}
//		}()
//		kexec.Command = cmds.Command
//
//		... test code ...
//
//	}
package fake

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/openconfig/kne/exec"
)

// A Response indicates how Command should respond to Run.
type Response struct {
	Cmd        string
	Args       []string
	Err        interface{}
	Stdout     string
	Stderr     string
	OutOfOrder bool // This response can be out of order
	Optional   bool // This response might not be used
}

func (r Response) String() string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "{Cmd: %q", r.Cmd)
	if len(r.Args) > 0 {
		fmt.Fprintf(&buf, ", Args: []string{%q", r.Args[0])
		for _, arg := range r.Args[1:] {
			fmt.Fprintf(&buf, ", %q", arg)
		}
		fmt.Fprintf(&buf, "}")
	}
	if r.Stdout != "" {
		fmt.Fprintf(&buf, ", Stdout: %q", r.Stdout)
	}
	if r.Stderr != "" {
		fmt.Fprintf(&buf, ", Stderr: %q", r.Stderr)
	}
	if r.Err != nil {
		fmt.Fprintf(&buf, ", Err: %q", r.Err)
	}
	if r.OutOfOrder {
		fmt.Fprintf(&buf, ", OutOfOrder: true")
	}
	if r.Optional {
		fmt.Fprintf(&buf, ", Optional: true")
	}
	fmt.Fprintf(&buf, "}")
	return buf.String()
}

// A Command is an implementation of exec.Cmd that is used to return
// predefined results when exec.Cmd.Run is called.
type Command struct {
	Name       string // if set it is included in errors
	cmd        string
	args       []string
	responses  []Response
	unexpected []Response
	stdout     io.Writer
	stderr     io.Writer
	stdin      io.Reader
	cnt        int
}

// Commands returns a Command that is primed with the provided responses.
// It's Command method can be used to override exec.Command.
func Commands(resp []Response) *Command {
	return &Command{
		responses: resp,
	}
}

// Command resets the command associated with c.
func (c *Command) Command(cmd string, args ...string) exec.Cmd {
	c.cmd = cmd
	c.args = args
	return c
}

// Stdout sets standard out to w.
func (c *Command) SetStdout(w io.Writer) { c.stdout = w }

// Stderr sets standard err to w.
func (c *Command) SetStderr(w io.Writer) { c.stderr = w }

// Stdin sets standard in to r.
func (c *Command) SetStdin(r io.Reader) { c.stdin = r }

// LogCommand is called with the string representation of the command that is
// running.  The test program can optionally set this to their own function.
var LogCommand = func(string) {}

// Run runs the command.  It expects there to be a corresponding Response to the
// command.  If the command does not match the first remaining response Run
// searches for a matching Response that has OutOfOrder set to true.  It also
// calls LogCommand with a string representation of a Response that matches this
// command.
//
// Run returns nil if no matching response is found.  Use c.Done to detect
// these errors.
func (c *Command) Run() error {
	c.cnt++
	call := Response{
		Cmd:  c.cmd,
		Args: c.args,
	}

	defer func() {
		LogCommand(call.String())
	}()
	if len(c.responses) == 0 {
		c.unexpected = append(c.unexpected, Response{Cmd: c.cmd, Args: c.args})
		return nil
	}

	// Always check to see if we match the next expected response.
	// If we don't then look to see if there is an OutOfOrder response that we match.
	r := c.responses[0]
	if c.matches(r) {
		c.responses = c.responses[1:]
	} else {
		matched := false
		var i int
		for i, r = range c.responses {
			if r.OutOfOrder && c.matches(r) {
				matched = true
				c.responses = append(c.responses[:i], c.responses[i+1:]...)
				break
			}
		}
		if !matched {
			c.unexpected = append(c.unexpected, Response{Cmd: c.cmd, Args: c.args})
			return nil
		}
	}

	// r is now the matching Response.

	call.Stdout = r.Stdout
	call.Stderr = r.Stderr
	call.Err = r.Err
	call.OutOfOrder = r.OutOfOrder
	call.Optional = r.Optional

	if c.stdout != nil && r.Stdout != "" {
		fmt.Fprintf(c.stdout, r.Stdout)
	}
	if c.stderr != nil && r.Stderr != "" {
		fmt.Fprintf(c.stderr, r.Stderr)
	}
	switch e := r.Err.(type) {
	case string:
		return errors.New(e)
	case error:
		return e
	default:
		return nil
	}
}

// A DoneError is returned when the calls to a Command do not match the Responses.
type DoneError struct {
	Source     string
	Unexpected []Response // Unexpected calls
	Unused     []Response // Unused calls
}

func (e *DoneError) Error() string {
	var buf strings.Builder
	if e.Source != "" {
		fmt.Fprintf(&buf, "%s: ", e.Source)
	}
	if len(e.Unused) > 0 {
		fmt.Fprintf(&buf, "didn't execute:")
		for _, r := range e.Unused {
			fmt.Fprintf(&buf, "\n\t%v", r)
		}
	}
	if len(e.Unexpected) > 0 {
		if len(e.Unused) > 0 {
			fmt.Fprintln(&buf)
		}
		fmt.Fprintf(&buf, "unexpected executions:")
		for _, r := range e.Unexpected {
			fmt.Fprintf(&buf, "\n\t%v", r)
		}
	}
	return buf.String()
}

// Done returns an error if there were any unexpected commands called on c or if
// there are any non-optional responses left.
//
// Done should be called once the test has finished calling c.Command.
func (c *Command) Done() error {
	left := c.left()
	if len(left) == 0 && len(c.unexpected) == 0 {
		return nil
	}
	src := "Done"
	if c.Name != "" {
		src = c.Name + ".Done"
	}
	return &DoneError{
		Source:     src,
		Unexpected: c.unexpected,
		Unused:     left,
	}
}

// left returns any non-optional unused responses
func (c *Command) left() []Response {
	var resp []Response
	for _, r := range c.responses {
		if !r.Optional {
			resp = append(resp, r)
		}
	}
	return resp
}

// matches returns true if the current command in c matches r.
func (c *Command) matches(r Response) bool {
	if c.cmd != r.Cmd && r.Cmd != "" {
		return false
	}
	return compareArgs(c.args, r.Args)
}

// compareArgs compares the two list of arguments to determin if they are the
// same or not.  The values in wantArgs can have a ".*" as the suffix or prefix
// to indicate a prefix or suffix match should be used instead of equality.
func compareArgs(gotArgs, wantArgs []string) bool {
	if len(gotArgs) != len(wantArgs) {
		return false
	}
	for i, got := range gotArgs {
		want := wantArgs[i]
		switch {
		case got == want:
			continue
		case strings.HasPrefix(want, ".*"):
			if strings.HasSuffix(got, want[2:]) {
				continue
			}
		case strings.HasSuffix(want, ".*"):
			if strings.HasPrefix(got, want[:len(want)-2]) {
				continue
			}
		}
		return false
	}
	return true
}
