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
	klog.InitFlags(nil)
	// Default logtostderr to off rather than on.
	if f := flag.Lookup("logtostderr"); f != nil {
		f.Value.Set("false")
		f.DefValue = "false"
	}
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		klog.Flush()
		os.Exit(1)
	}
	klog.Flush()
}
