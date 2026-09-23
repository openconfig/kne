package run

import (
	"bytes"
	"context"
	"io"

	kexec "github.com/openconfig/kne/exec"
	"github.com/openconfig/kne/logshim"
	log "k8s.io/klog/v2"
)

var (
	logInfo    = log.Info
	logWarning = log.Warning
)

// labelKey is the context key under which a command label is stored.
type labelKey struct{}

// WithLabel returns a copy of ctx that tags the output of commands run with it
// as coming from label.  Callers that run commands concurrently should set a
// label; without one the interleaved output of several commands is
// indistinguishable, since every line is attributed only to the binary that
// produced it.
//
// The label affects logging only.  It does not tie the command's lifetime to
// ctx: commands are not canceled when ctx ends.
func WithLabel(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, labelKey{}, label)
}

// Label returns the label attached to ctx by WithLabel, or "" if there is
// none.  Components can use it to tag their own log messages the same way
// their command output is tagged.
func Label(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(labelKey{}).(string)
	return s
}

// logPrefix returns the prefix to put in front of the output of cmd.
func logPrefix(ctx context.Context, cmd string) string {
	if l := Label(ctx); l != "" {
		return "(" + cmd + "/" + l + "): "
	}
	return "(" + cmd + "): "
}

// runCommand is a wrapper utility function that creates and runs a command with
// various inputs and settings.
func runCommand(ctx context.Context, writeLogs bool, in []byte, cmd string, args ...string) ([]byte, error) {
	c := kexec.Command(cmd, args...)
	var out bytes.Buffer
	c.SetStdout(&out)
	c.SetStderr(&out)
	if writeLogs {
		prefix := logPrefix(ctx, cmd)
		outLog := logshim.New(func(v ...interface{}) {
			logInfo(append([]interface{}{prefix}, v...)...)
		})
		errLog := logshim.New(func(v ...interface{}) {
			logWarning(append([]interface{}{prefix}, v...)...)
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
	_, err := runCommand(context.Background(), true, nil, cmd, args...)
	return err
}

// LogCommandContext is LogCommand with any label attached to ctx applied to
// the logged output.
func LogCommandContext(ctx context.Context, cmd string, args ...string) error {
	_, err := runCommand(ctx, true, nil, cmd, args...)
	return err
}

// LogCommandWithInput runs the specified command but records standard output
// with log.Info and standard error with log.Warning. in is sent to
// the standard input of the command.
func LogCommandWithInput(in []byte, cmd string, args ...string) error {
	_, err := runCommand(context.Background(), true, in, cmd, args...)
	return err
}

// OutLogCommand runs the specified command but records standard output
// with log.Info and standard error with log.Warning. Standard output
// and standard error are also returned.
func OutLogCommand(cmd string, args ...string) ([]byte, error) {
	return runCommand(context.Background(), true, nil, cmd, args...)
}

// OutCommand runs the specified command and returns any standard output
// as well as any errors.
func OutCommand(cmd string, args ...string) ([]byte, error) {
	return runCommand(context.Background(), false, nil, cmd, args...)
}
