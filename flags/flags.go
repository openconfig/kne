// Copyright 2023 Google LLC
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

// Package flags imports command line flags from other flag packages into pflag.
// This package can be replaced to easily import command line flags from a
// different flag package.
package flags

import (
	"flag"

	"github.com/spf13/pflag"
	"k8s.io/klog/v2"
)

// Import imports the command line flags from the standard flag package into
// pflag's command line flags.
func Import(defmap map[string]string) {
	klog.InitFlags(nil)
	// Opt into the new klog behavior so that -stderrthreshold is honored even
	// when -logtostderr=true (the default).
	// Ref: kubernetes/klog#212, kubernetes/klog#432
	flag.Set("legacy_stderr_threshold_behavior", "false") //nolint:errcheck
	flag.Set("stderrthreshold", "INFO")                   //nolint:errcheck
	for k, v := range defmap {
		if f := flag.Lookup(k); f != nil {
			f.Value.Set(v)
			f.DefValue = v
		}
	}
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
}
