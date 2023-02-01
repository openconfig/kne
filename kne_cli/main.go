// Copyright 2021 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/openconfig/kne/cmd"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

func main() {
	// By default, send logs to files and the screen.
	// TODO(borman): rework what goes to the screen
	// in a nicer format.
	klog.InitFlags(nil)
	for k, v := range map[string]string{
		"logtostderr":     "false",
		"alsologtostderr": "false",
		"stderrthreshold": "info",
	} {
		if f := flag.Lookup(k); f != nil {
			f.Value.Set(v)
			f.DefValue = v
		}
	}
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	err := cmd.ExecuteContext(context.Background())
	flushLogs()
	if err != nil {
		os.Exit(1)
	}
}

// flushLogs flushes the logs to storage and then displays a list of
// log files associated with this run.
func flushLogs() {
	klog.Flush()

	// klog provides no mechanism to determine where the logs are.  This
	// code mimics the decsion that klog makes to determine the logging
	// directory.

	f := flag.Lookup("log_dir")
	var logdir string
	if f != nil {
		logdir = f.Value.String()
	}
	if logdir == "" {
		logdir = os.TempDir()
	}

	// The log files are named command.*.pid, but filepath.Glob only
	// supports expanding * to full component names.
	fd, err := os.Open(logdir)
	if err != nil {
		return
	}
	defer fd.Close()
	cmd := filepath.Base(os.Args[0]) + "."
	pid := "." + strconv.Itoa(os.Getpid())
	var files []string
	for {
		names, err := fd.Readdirnames(100)
		for _, name := range names {
			if strings.HasPrefix(name, cmd) && strings.HasSuffix(name, pid) {
				files = append(files, name)
			}
		}
		if err != nil {
			break
		}
	}
	if len(files) > 0 {
		sort.Strings(files)
		fmt.Fprintf(os.Stderr, "Log files can be found in:\n")
		for _, file := range files {
			fmt.Fprintf(os.Stderr, "    %s\n", filepath.Join(logdir, file))
		}
	}
}
