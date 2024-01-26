package run

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/openconfig/gnmi/errdiff"
	kexec "github.com/openconfig/kne/exec"
	fexec "github.com/openconfig/kne/exec/fake"
)

func TestRunCommand(t *testing.T) {
	tests := []struct {
		desc         string
		writeLogs    bool
		in           string
		cmd          string
		args         []string
		resp         []fexec.Response
		want         string
		wantInfos    string
		wantWarnings string
		wantErr      string
	}{{
		desc:      "log no input",
		writeLogs: true,
		cmd:       "echo",
		args:      []string{"hello"},
		resp: []fexec.Response{
			{Cmd: "echo", Args: []string{"hello"}, Stdout: "hello"},
		},
		want:      "hello",
		wantInfos: "(echo): hello",
	}, {
		desc:      "log no input with stderr",
		writeLogs: true,
		cmd:       "echo",
		args:      []string{"hello"},
		resp: []fexec.Response{
			{Cmd: "echo", Args: []string{"hello"}, Stdout: "hello", Stderr: "err"},
		},
		want:         "helloerr",
		wantInfos:    "(echo): hello",
		wantWarnings: "(echo): err",
	}, {
		desc:      "log with input",
		writeLogs: true,
		in:        "echo hello",
		cmd:       "cat",
		resp: []fexec.Response{
			{Cmd: "cat", Stdout: "hello"},
		},
		want:      "hello",
		wantInfos: "(cat): hello",
	}, {
		desc: "no log no input",
		cmd:  "echo",
		args: []string{"hello"},
		resp: []fexec.Response{
			{Cmd: "echo", Args: []string{"hello"}, Stdout: "hello"},
		},
		want: "hello",
	}, {
		desc: "no log with input",
		in:   "echo hello",
		cmd:  "cat",
		resp: []fexec.Response{
			{Cmd: "cat", Stdout: "hello"},
		},
		want: "hello",
	}, {
		desc: "failed command",
		cmd:  "false",
		resp: []fexec.Response{
			{Cmd: "false", Err: "failure"},
		},
		wantErr: "failure",
	}}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			fexec.LogCommand = func(s string) {
				t.Logf("%s: %s", tt.desc, s)
			}
			cmds := fexec.Commands(tt.resp)
			kexec.Command = cmds.Command
			defer checkCmds(t, cmds)

			origLogInfo := logInfo
			defer func() {
				logInfo = origLogInfo
			}()
			var infos bytes.Buffer
			logInfo = func(args ...interface{}) {
				fmt.Fprint(&infos, args...)
			}

			origLogWarning := logWarning
			defer func() {
				logWarning = origLogWarning
			}()
			var warnings bytes.Buffer
			logWarning = func(args ...interface{}) {
				fmt.Fprint(&warnings, args...)
			}

			got, err := runCommand(tt.writeLogs, []byte(tt.in), tt.cmd, tt.args...)
			if s := errdiff.Substring(err, tt.wantErr); s != "" {
				t.Fatalf("unexpected error: %s", s)
			}
			if string(got) != tt.want {
				t.Errorf("runCommand() got output %v, want %v", string(got), tt.want)
			}
			if string(infos.Bytes()) != tt.wantInfos {
				t.Errorf("runCommand() got info logs %v, want %v", string(infos.Bytes()), tt.wantInfos)
			}
			if string(warnings.Bytes()) != tt.wantWarnings {
				t.Errorf("runCommand() got warning logs %v, want %v", string(warnings.Bytes()), tt.wantWarnings)
			}
		})
	}
}

func checkCmds(t *testing.T, cmds *fexec.Command) {
	t.Helper()
	if err := cmds.Done(); err != nil {
		t.Errorf("%v", err)
	}
}
