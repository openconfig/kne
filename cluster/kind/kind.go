package kind

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/openconfig/kne/exec/run"
	"golang.org/x/oauth2/google"
	log "k8s.io/klog/v2"
)

const (
	dockerConfigEnvVar = "DOCKER_CONFIG"
	kubeletConfigPath  = "/var/lib/kubelet/config.json"
)

var (
	kubeletConfigPathTemplate    = "%s:" + kubeletConfigPath
	googleFindDefaultCredentials = google.FindDefaultCredentials
	clusterKindNameRE            = regexp.MustCompile("kind-(.*)")
)

func ClusterIsKind() (bool, error) {
	name, err := clusterName()
	if err != nil {
		return false, fmt.Errorf("unable to determine cluster name: %v", err)
	}
	matches := clusterKindNameRE.FindStringSubmatch(name)
	if len(matches) != 2 || matches[1] == "" {
		return false, nil
	}
	return true, nil
}

func clusterName() (string, error) {
	b, err := run.OutCommand("kubectl", "config", "current-context")
	if err != nil {
		return "", fmt.Errorf("unable to determine cluster information: %v", err)
	}
	return string(b), nil
}

func clusterKindName() (string, error) {
	name, err := clusterName()
	if err != nil {
		return "", fmt.Errorf("unable to determine cluster name: %v", err)
	}
	matches := clusterKindNameRE.FindStringSubmatch(name)
	if len(matches) != 2 || matches[1] == "" {
		return "", fmt.Errorf("cluster %v is not a kind cluster", name)
	}
	return matches[1], nil
}

func clusterKindNodes() ([]string, error) {
	name, err := clusterKindName()
	if err != nil {
		return nil, err
	}
	out, err := run.OutCommand("kind", "get", "nodes", "--name", name)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return []string{}, nil
	}
	nodes := []string{}
	for _, n := range strings.Split(string(out), " ") {
		nodes = append(nodes, strings.TrimSuffix(n, "\n"))
	}
	return nodes, nil
}

func SetupGARAccess(ctx context.Context, registries []string) error {
	// Create a temporary dir to hold a new docker config that lacks credsStore.
	// Then use `docker login` to store the generated credentials directly in
	// the temporary docker config.
	// See https://kind.sigs.k8s.io/docs/user/private-registries/#use-an-access-token
	// for more information.
	tempDockerDir, err := os.MkdirTemp("", "kne_kind_docker")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDockerDir)
	originalConfig := os.Getenv(dockerConfigEnvVar)
	defer os.Setenv(dockerConfigEnvVar, originalConfig)
	if err := os.Setenv(dockerConfigEnvVar, tempDockerDir); err != nil {
		return err
	}
	configPath := filepath.Join(tempDockerDir, "config.json")
	if err := writeDockerConfig(configPath, registries); err != nil {
		return err
	}
	creds, err := googleFindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("failed to find gcloud credentials: %v", err)
	}
	token, err := creds.TokenSource.Token()
	if err != nil {
		return fmt.Errorf("failed to get token from gcloud credentials: %v", err)
	}
	for _, r := range registries {
		s := fmt.Sprintf("https://%s", r)
		if err := run.LogCommandWithInput([]byte(token.AccessToken), "docker", "login", "-u", "oauth2accesstoken", "--password-stdin", s); err != nil {
			return err
		}
	}
	// Copy the new docker config to each node and restart kubelet so it
	// picks up the new config that contains the embedded credentials.
	nodes, err := clusterKindNodes()
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := run.LogCommand("docker", "cp", configPath, fmt.Sprintf(kubeletConfigPathTemplate, node)); err != nil {
			return err
		}
		if err := run.LogCommand("docker", "exec", node, "systemctl", "restart", "kubelet.service"); err != nil {
			return err
		}
	}
	log.Infof("Setup GAR access for %v", registries)
	return nil
}

func RefreshGARAccess(ctx context.Context) error {
	nodes, err := clusterKindNodes()
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		log.Infof("No kind nodes found in cluster, no GAR access to refresh")
		return nil
	}
	cfg, err := run.OutCommand("docker", "exec", nodes[0], "cat", kubeletConfigPath)
	if err != nil {
		log.Infof("Kubelet docker config not found on %v, no GAR access to refresh", nodes[0])
		return nil
	}
	var dCfg DockerConfig
	if err := json.Unmarshal(cfg, &dCfg); err != nil {
		return err
	}
	var registries []string
	for r := range dCfg.Auths {
		registries = append(registries, r)
	}
	log.Infof("Refreshing GAR access for %v", registries)
	return SetupGARAccess(ctx, registries)
}

type DockerConfig struct {
	Auths map[string]struct{} `json:"auths"`
}

func writeDockerConfig(path string, registries []string) error {
	dc := &DockerConfig{Auths: map[string]struct{}{}}
	for _, r := range registries {
		dc.Auths[r] = struct{}{}
	}
	b, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return err
	}
	return nil
}
