// Package exec wraps os/exec.  The subpackage fake can be used in testing
// to fake the call to a command.
package exec

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	matches := func(got, want string) bool {
		if got == want {
			return true
		}
		if want == "" {
			return false
		}
		return strings.Contains(got, want)
	}

	for _, tt := range []struct {
		name   string
		cmd    string
		args   []string
		stdout string
		stderr string
		err    string
	}{
		{
			name: "successful command with no output",
			cmd:  "true",
		},
		{
			name: "failed command with no output",
			cmd:  "false",
			err:  "failed: exit status 1",
		},
		{
			name:   "successful command with output",
			cmd:    "echo",
			args:   []string{"one", "two"},
			stdout: "one two\n",
		},
		{
			name: "bad argument with stderr",
			// We call ourselves as we know what the output
			// should be.
			cmd:    os.Args[0],
			args:   []string{"--bad"},
			stderr: "flag provided but not defined: -bad\n",
			err:    fmt.Sprintf(`"%s --bad"`, os.Args[0]),
		},
		{
			name: "invalid command",
			cmd:  "no-such-command",
			err:  `"no-such-command" failed: exec`,
		},
	} {
		stdout.Reset()
		stderr.Reset()
		if tt.err == "" {
			tt.err = "nil"
		}
		t.Run(tt.name, func(t *testing.T) {
			c := Command(tt.cmd, tt.args...)
			c.SetStdout(&stdout)
			c.SetStderr(&stderr)
			got := "nil"
			if err := c.Run(); err != nil {
				got = err.Error()
			}
			if !matches(got, tt.err) {
				t.Errorf("Got error\n%v\n, want\n%v", got, tt.err)
			}
			if got, want := stdout.String(), tt.stdout; !matches(got, want) {
				t.Errorf("Got stdout %q, want %q", got, want)
			}
			if got, want := stderr.String(), tt.stderr; !matches(got, want) {
				t.Errorf("Got stderr %q, want %q", got, want)
			}
		})
	}
}
