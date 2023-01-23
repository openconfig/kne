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
	"os"

	"github.com/openconfig/kne/cmd"
	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

func main() {
	// By default, send logs to files and the screen.
	// TODO(borman): rework what goes to the screen
	klog.InitFlags(nil)
	// in a nicer format.
	for k, v := range map[string]string{
		"logtostderr":     "false",
		"alsologtostderr": "true",
		"stderrthreshold": "info",
	} {
		if f := flag.Lookup(k); f != nil {
			f.Value.Set(v)
			f.DefValue = v
		}
	}
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		klog.Flush()
		os.Exit(1)
	}
	klog.Flush()
}
