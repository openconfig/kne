// Binary dsdn-webhook is an mutating k8s webhook
// (https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"google3/net/sdn/decentralized/kne/webhook/admission/admission"
	admissionv1 "google3/third_party/golang/k8s_io/api/v/v0_23/admission/v1/v1"
	"google3/third_party/golang/klog_v2/klog"
)

const (
	cert = "/etc/dsdn-assembly-webhook/tls/tls.crt"
	key  = "/etc/dsdn-assembly-webhook/tls/tls.key"
)

func main() {
	http.HandleFunc("/mutate-pods", ServeMutatePods)
	http.HandleFunc("/health", ServeHealth)

	klog.Info("Listening on port 443...")
	klog.Fatal(http.ListenAndServeTLS(":443", cert, key, nil))
}

// ServeHealth returns 200 when things are good
func ServeHealth(w http.ResponseWriter, r *http.Request) {
	klog.Infof("uri %s - healthy", r.RequestURI)
	fmt.Fprint(w, "OK")
}

// ServeMutatePods returns an admission review with pod mutations as a json patch
// in the review response
func ServeMutatePods(w http.ResponseWriter, r *http.Request) {
	in, err := parseRequest(*r)
	if err != nil {
		klog.Error(err)
		return
	}

	admitter := admission.New(in.Request)

	res, err := admitter.Review()
	if err != nil {
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
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, fmt.Errorf("Content-Type: %q should be %q",
			r.Header.Get("Content-Type"), "application/json")
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
