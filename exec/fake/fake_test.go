package fake

import (
	"errors"
	"strings"
	"testing"
)

var (
	err1 = errors.New("err1")
	err2 = errors.New("err2")
)

func TestResponseString(t *testing.T) {
	for _, tt := range []struct {
		in  Response
		out string
	}{
		{in: Response{}, out: `{Cmd: ""}`},
		{in: Response{Cmd: "cmd1"}, out: `{Cmd: "cmd1"}`},
		{in: Response{Cmd: "cmd2", Args: []string{"1", "2"}}, out: `{Cmd: "cmd2", Args: []string{"1", "2"}}`},
		{in: Response{Cmd: "cmd3", Stdout: "output"}, out: `{Cmd: "cmd3", Stdout: "output"}`},
		{in: Response{Cmd: "cmd4", Stderr: "error"}, out: `{Cmd: "cmd4", Stderr: "error"}`},
		{in: Response{Cmd: "cmd5", Err: err1}, out: `{Cmd: "cmd5", Err: "err1"}`},
		{in: Response{Cmd: "cmd6", OutOfOrder: true}, out: `{Cmd: "cmd6", OutOfOrder: true}`},
		{in: Response{Cmd: "cmd7", Optional: true}, out: `{Cmd: "cmd7", Optional: true}`},
	} {
		out := tt.in.String()
		if out != tt.out {
			t.Errorf("%v got:\n%s\nwant:\n%s", tt.in, out, tt.out)
		}
	}
}

func TestSuccessfulCommands(t *testing.T) {
	for _, tt := range []struct {
		name string
		send []Response
		resp []Response
	}{
		{
			name: "one_command",
			send: []Response{
				{Cmd: "cmd1"},
			},
			resp: []Response{
				{Cmd: "cmd1"},
			},
		}, {
			name: "two_commands",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
			},
			resp: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
			},
		}, {
			name: "two_commands_first_error",
			send: []Response{
				{Cmd: "cmd1", Err: err1},
				{Cmd: "cmd2"},
			},
			resp: []Response{
				{Cmd: "cmd1", Err: err1},
				{Cmd: "cmd2"},
			},
		}, {
			name: "two_commands_second_error",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2", Err: err2},
			},
			resp: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2", Err: err2},
			},
		}, {
			name: "two_commands_both_error",
			send: []Response{
				{Cmd: "cmd1", Err: err1},
				{Cmd: "cmd2", Err: err2},
			},
			resp: []Response{
				{Cmd: "cmd1", Err: err1},
				{Cmd: "cmd2", Err: err2},
			},
		}, {
			name: "test_output",
			send: []Response{
				{Cmd: "cmd1", Stdout: "output"},
				{Cmd: "cmd2", Stderr: "error"},
				{Cmd: "cmd3", Stdout: "output", Stderr: "error"},
			},
			resp: []Response{
				{Cmd: "cmd1", Stdout: "output"},
				{Cmd: "cmd2", Stderr: "error"},
				{Cmd: "cmd3", Stdout: "output", Stderr: "error"},
			},
		}, {
			name: "out_of_order",
			send: []Response{
				{Cmd: "cmd2"},
				{Cmd: "cmd1"},
			},
			resp: []Response{
				{Cmd: "cmd1", OutOfOrder: true},
				{Cmd: "cmd2", OutOfOrder: true},
			},
		}, {
			name: "partial_of_order",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
				{Cmd: "cmd3"},
				{Cmd: "cmd4"},
			},
			resp: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd3"},
				{Cmd: "cmd4"},
				{Cmd: "cmd2", OutOfOrder: true},
			},
		}, {
			name: "optional",
			send: []Response{
				{Cmd: "cmd1"},
			},
			resp: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2", Optional: true},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmds := Commands(tt.resp)
			defer func() {
				if err := cmds.Done(); err != nil {
					t.Errorf("%v", err)
				}
			}()
			for i, cmd := range tt.send {
				var stdout, stderr strings.Builder
				c := cmds.Command(cmd.Cmd, cmd.Args...)
				c.SetStdout(&stdout)
				c.SetStderr(&stderr)
				if err := c.Run(); err != cmd.Err {
					t.Errorf("#%d: got error %v, want %v", i, err, cmd.Err)
				}
				if stdout.String() != cmd.Stdout {
					t.Errorf("#%d: got stdout %q, want %q", i, stdout.String(), cmd.Stdout)
				}
				if stderr.String() != cmd.Stderr {
					t.Errorf("#%d: got stderr %q, want %q", i, stderr.String(), cmd.Stderr)
				}
			}
		})
	}
}

func TestFailed(t *testing.T) {
	for _, tt := range []struct {
		name       string
		send       []Response
		unused     []Response
		resp       []Response
		unexpected []Response

		// err is our expected "unexpected" error.
		// This is used for all cases.
		err error
	}{
		{
			name: "one_command",
			send: []Response{
				{Cmd: "wrong", Args: []string{"command"}},
			},
			unused: []Response{
				{Cmd: "right", Args: []string{"command"}, Err: err1},
			},
			resp: []Response{
				{Cmd: "right", Args: []string{"command"}, Err: err1},
			},
			unexpected: []Response{
				{Cmd: "wrong", Args: []string{"command"}},
			},
		}, {
			name: "two_commands",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
			},
			unused: []Response{
				{Cmd: "cmd4"},
			},
			resp: []Response{
				{Cmd: "cmd2"},
				{Cmd: "cmd4"},
			},
			unexpected: []Response{
				{Cmd: "cmd1"},
			},
		}, {
			name: "unexpected_err",
			send: []Response{
				{Cmd: "cmd1", Err: err1},
			},
			resp: []Response{
				{Cmd: "cmd1"},
			},
		}, {
			name: "expected_err",
			send: []Response{
				{Cmd: "cmd1"},
			},
			resp: []Response{
				{Cmd: "cmd1", Err: err1},
			},
			err: err1,
		}, {
			name: "expected_errs",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
			},
			resp: []Response{
				{Cmd: "cmd1", Err: err1},
				{Cmd: "cmd2", Err: err1},
			},
			err: err1,
		}, {
			name: "mixed_errs",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2", Err: err2},
			},
			resp: []Response{
				{Cmd: "cmd1", Err: err1},
				{Cmd: "cmd2", Err: err2},
			},
			err: err1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmds := Commands(tt.resp)
			for _, cmd := range tt.send {
				var stdout, stderr strings.Builder
				c := cmds.Command(cmd.Cmd, cmd.Args...)
				c.SetStdout(&stdout)
				c.SetStderr(&stderr)
				if err := c.Run(); err != tt.err {
					if err != cmd.Err {
						t.Errorf("%s: got error %v, want %v", cmd.Cmd, err, tt.err)
					}
				}
				if stdout.String() != cmd.Stdout {
					t.Errorf("%s: got stdout %q, want %q", cmd.Cmd, stdout.String(), cmd.Stdout)
				}
				if stderr.String() != cmd.Stderr {
					t.Errorf("%s: got stderr %q, want %q", cmd.Cmd, stderr.String(), cmd.Stderr)
				}
			}
			for _, u := range cmds.unexpected {
				var ue Response
				if len(tt.unexpected) > 0 {
					ue = tt.unexpected[0]
				}
				t.Logf("Compare %v and %v", u, ue)
				if len(tt.unexpected) > 0 && tt.unexpected[0].String() == u.String() {
					tt.unexpected = tt.unexpected[1:]
					continue
				}
				t.Errorf("Uncalled command: %v", u)
			}
			for _, u := range tt.unexpected {
				t.Errorf("Did not get unexpected call %v", u)
			}
			for _, got := range cmds.responses {
				if len(tt.unused) == 0 {
					if !got.Optional {
						t.Errorf("Did not call: %v", got)
					}
					continue
				}
				want := tt.unused[0]
				tt.unused = tt.unused[1:]
				if want.String() != got.String() {
					if !got.Optional {
						t.Errorf("Did not call: %v, expected %v", got, want)
					}
				}
			}
			for _, u := range tt.unused {
				t.Errorf("extra: %v", u)
			}
		})
	}
}

func TestDone(t *testing.T) {
	for _, tt := range []struct {
		name string
		send []Response
		resp []Response
		done string

		// err is our expected "unexpected" error.
		// This is used for all cases.
		err error
	}{
		{
			name: "one_unexpected_command",
			send: []Response{
				{Cmd: "wrong", Args: []string{"command"}},
			},
			done: `test.Done: unexpected executions:
	{Cmd: "wrong", Args: []string{"command"}}`,
		}, {
			name: "two_unexpected_commands",
			send: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
			},
			done: `test.Done: unexpected executions:
	{Cmd: "cmd1"}
	{Cmd: "cmd2"}`,
		}, {
			name: "one_missing_command",
			resp: []Response{
				{Cmd: "cmd1"},
			},
			done: `test.Done: didn't execute:
	{Cmd: "cmd1"}`,
		}, {
			name: "two_missing_commands",
			resp: []Response{
				{Cmd: "cmd1"},
				{Cmd: "cmd2"},
			},
			done: `test.Done: didn't execute:
	{Cmd: "cmd1"}
	{Cmd: "cmd2"}`,
		}, {
			name: "missing_and_unexpected_commands",
			send: []Response{
				{Cmd: "cmd1"},
			},
			resp: []Response{
				{Cmd: "cmd2"},
			},
			done: `test.Done: didn't execute:
	{Cmd: "cmd2"}
unexpected executions:
	{Cmd: "cmd1"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmds := Commands(tt.resp)
			cmds.Name = "test"
			for _, cmd := range tt.send {
				var stdout, stderr strings.Builder
				c := cmds.Command(cmd.Cmd, cmd.Args...)
				c.SetStdout(&stdout)
				c.SetStderr(&stderr)
				c.Run()
			}
			err := cmds.Done()
			if err.Error() != tt.done {
				t.Errorf("got %s, want %s", err, tt.done)
			}
		})
	}
}
