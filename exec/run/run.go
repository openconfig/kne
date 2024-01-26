package run

import (
	"bytes"
	"io"

	kexec "github.com/openconfig/kne/exec"
	"github.com/openconfig/kne/logshim"
	log "k8s.io/klog/v2"
)

var (
	logInfo    = log.Info
	logWarning = log.Warning
)

// runCommand is a wrapper utility function that creates and runs a command with
// various inputs and settings.
func runCommand(writeLogs bool, in []byte, cmd string, args ...string) ([]byte, error) {
	c := kexec.Command(cmd, args...)
	var out bytes.Buffer
	c.SetStdout(&out)
	c.SetStderr(&out)
	if writeLogs {
		outLog := logshim.New(func(v ...interface{}) {
			logInfo(append([]interface{}{"(" + cmd + "): "}, v...)...)
		})
		errLog := logshim.New(func(v ...interface{}) {
			logWarning(append([]interface{}{"(" + cmd + "): "}, v...)...)
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
