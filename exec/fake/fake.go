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
//
// A Command is safe for concurrent use.  Each call to Command returns an
// independent exec.Cmd, and the responses are consumed under a lock, so code
// under test may run commands from multiple goroutines.  Note that responses
// are still matched in order by default: when the commands may be issued
// concurrently, the order they arrive in is not deterministic, so the
// corresponding responses must be marked OutOfOrder.
package fake

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

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

// A Command hands out exec.Cmd implementations that return predefined results
// when exec.Cmd.Run is called.  The zero value is not useful; use Commands.
//
// A Command is safe for concurrent use by multiple goroutines.
type Command struct {
	Name string // if set it is included in errors

	mu         sync.Mutex
	responses  []Response
	unexpected []Response
	cnt        int
}

// An invocation is a single command produced by Command.Command.  Each
// invocation holds its own stdio so that concurrent commands do not interfere
// with each other; the shared response bookkeeping lives on the parent.
type invocation struct {
	parent *Command
	cmd    string
	args   []string
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
}

// Commands returns a Command that is primed with the provided responses.
// It's Command method can be used to override exec.Command.
func Commands(resp []Response) *Command {
	return &Command{
		responses: resp,
	}
}

// Command returns a new exec.Cmd that draws its result from c when run.
func (c *Command) Command(cmd string, args ...string) exec.Cmd {
	return &invocation{
		parent: c,
		cmd:    cmd,
		args:   args,
	}
}

// Stdout sets standard out to w.
func (i *invocation) SetStdout(w io.Writer) { i.stdout = w }

// Stderr sets standard err to w.
func (i *invocation) SetStderr(w io.Writer) { i.stderr = w }

// Stdin sets standard in to r.
func (i *invocation) SetStdin(r io.Reader) { i.stdin = r }

// LogCommand is called with the string representation of the command that is
// running.  The test program can optionally set this to their own function.
var LogCommand = func(string) {}

// Run runs the command.  It expects there to be a corresponding Response to the
// command.  If the command does not match the first remaining response Run
// searches for a matching Response that has OutOfOrder set to true.  It also
// calls LogCommand with a string representation of a Response that matches this
// command.
//
// Run returns nil if no matching response is found.  Use Command.Done to detect
// these errors.
func (i *invocation) Run() error {
	return i.parent.run(i)
}

// run consumes the response matching i.  The lock is held for the whole call,
// which both keeps the response bookkeeping consistent and serializes the
// LogCommand callback, so a test hook that records commands does not need its
// own synchronization.
func (c *Command) run(i *invocation) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cnt++
	call := Response{
		Cmd:  i.cmd,
		Args: i.args,
	}

	defer func() {
		LogCommand(call.String())
	}()
	if len(c.responses) == 0 {
		c.unexpected = append(c.unexpected, Response{Cmd: i.cmd, Args: i.args})
		return nil
	}

	// Always check to see if we match the next expected response.
	// If we don't then look to see if there is an OutOfOrder response that we match.
	r := c.responses[0]
	if i.matches(r) {
		c.responses = c.responses[1:]
	} else {
		matched := false
		var n int
		for n, r = range c.responses {
			if r.OutOfOrder && i.matches(r) {
				matched = true
				c.responses = append(c.responses[:n], c.responses[n+1:]...)
				break
			}
		}
		if !matched {
			c.unexpected = append(c.unexpected, Response{Cmd: i.cmd, Args: i.args})
			return nil
		}
	}

	// r is now the matching Response.

	call.Stdout = r.Stdout
	call.Stderr = r.Stderr
	call.Err = r.Err
	call.OutOfOrder = r.OutOfOrder
	call.Optional = r.Optional

	// The writers belong to the caller, and a fake has nothing useful to do
	// about a failure to write to them, so the results are discarded.
	if i.stdout != nil && r.Stdout != "" {
		_, _ = fmt.Fprint(i.stdout, r.Stdout)
	}
	if i.stderr != nil && r.Stderr != "" {
		_, _ = fmt.Fprint(i.stderr, r.Stderr)
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
// Done should be called once the test has finished calling c.Command and all
// commands it handed out have finished running.
func (c *Command) Done() error {
	c.mu.Lock()
	defer c.mu.Unlock()

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

// left returns any non-optional unused responses.  c.mu must be held.
func (c *Command) left() []Response {
	var resp []Response
	for _, r := range c.responses {
		if !r.Optional {
			resp = append(resp, r)
		}
	}
	return resp
}

// matches returns true if the command in i matches r.
func (i *invocation) matches(r Response) bool {
	if i.cmd != r.Cmd && r.Cmd != "" {
		return false
	}
	return compareArgs(i.args, r.Args)
}

// compareArgs compares the two list of arguments to determine if they are the
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
