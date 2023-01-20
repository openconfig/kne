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
	"flag"
	"path/filepath"

	"github.com/spf13/pflag"
	"k8s.io/client-go/util/homedir"
	log "k8s.io/klog/v2"
)

var (
	defaultKubeCfg = ""
	kubecfg        = ""
)

func Init() {
	if home := homedir.HomeDir(); home != "" {
		defaultKubeCfg = filepath.Join(home, ".kube", "config")
	}
	pflag.StringVar(&kubecfg, "kubecfg", defaultKubeCfg, "Kubecfg to use")
}

func main() {
	log.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	Init()
	log.Flush()
}
