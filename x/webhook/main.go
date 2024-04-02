// Copyright 2024 Google LLC
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

// Main is an mutating k8s webhook
// (https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/openconfig/kne/x/webhook/admission"
	"github.com/openconfig/kne/x/webhook/examples/addcontainer"
	"github.com/openconfig/kne/x/webhook/mutate"
	admissionv1 "k8s.io/api/admission/v1"
	log "k8s.io/klog/v2"
)

const (
	cert = "/etc/kne-assembly-webhook/tls/tls.crt"
	key  = "/etc/kne-assembly-webhook/tls/tls.key"
)

func main() {
	http.HandleFunc("/mutate-objects", ServeMutateObjects)
	http.HandleFunc("/health", ServeHealth)

	log.Info("Listening on port 443...")
	s := http.Server{
		Addr:         ":443",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Fatal(s.ListenAndServeTLS(cert, key))
}

// ServeHealth returns 200 when things are good
func ServeHealth(w http.ResponseWriter, r *http.Request) {
	log.Infof("uri %s - healthy", r.RequestURI)
	fmt.Fprint(w, "OK")
}

// ServeMutateObjects returns an admission review with mutations as a json patch
// in the review response
func ServeMutateObjects(w http.ResponseWriter, r *http.Request) {
	in, err := parseRequest(*r)
	if err != nil {
		log.Error(err)
		return
	}

	admitter := admission.New(in.Request, []mutate.MutationFunc{addcontainer.AddContainer})

	res, err := admitter.Review()
	if err != nil {
		log.Errorf("admitter.Review() error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	jout, err := json.Marshal(res)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "%s", jout)
}

// parseRequest extracts an AdmissionReview from an http.Request if possible
func parseRequest(r http.Request) (*admissionv1.AdmissionReview, error) {
	log.Infof("parsing request: %v", r)
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, fmt.Errorf("Content-Type: %q should be %q", r.Header.Get("Content-Type"), "application/json")
	}

	bodybuf := new(bytes.Buffer)
	bodybuf.ReadFrom(r.Body)
	body := bodybuf.Bytes()

	if len(body) == 0 {
		return nil, fmt.Errorf("admission request body is empty")
	}

	var a admissionv1.AdmissionReview

	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("could not parse admission review request: %v", err)
	}

	if a.Request == nil {
		return nil, fmt.Errorf("admission review can't be used: Request field is nil")
	}

	return &a, nil
}
