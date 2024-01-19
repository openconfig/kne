package docker

import (
	"golang.org/x/oauth2/google"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	dockerConfigEnvVar        = "DOCKER_CONFIG"
	kubeletConfigPathTemplate = "%s:/var/lib/kubelet/config.json"
)

var (
	execLookPath                 = exec.LookPath
	googleFindDefaultCredentials = google.FindDefaultCredentials
)

func runCommand(writeLogs bool, in []byte, cmd string, args ...string) ([]byte, error) {
	c := kexec.Command(cmd, args...)
	var out bytes.Buffer
	c.SetStdout(&out)
	if writeLogs {
		outLog := logshim.New(func(v ...interface{}) {
			log.Info(append([]interface{}{"(" + cmd + "): "}, v...)...)
		})
		errLog := logshim.New(func(v ...interface{}) {
			log.Warning(append([]interface{}{"(" + cmd + "): "}, v...)...)
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

// logCommand runs the specified command but records standard output
// with log.Info and standard error with log.Warning.
func logCommand(cmd string, args ...string) error {
	_, err := runCommand(true, nil, cmd, args...)
	return err
}

// logCommandWithInput runs the specified command but records standard output
// with log.Info and standard error with log.Warning. in is sent to
// the standard input of the command.
func logCommandWithInput(in []byte, cmd string, args ...string) error {
	_, err := runCommand(true, in, cmd, args...)
	return err
}

// outLogCommand runs the specified command but records standard output
// with log.Info and standard error with log.Warning. Standard output
// and standard error are also returned.
func outLogCommand(cmd string, args ...string) ([]byte, error) {
	return runCommand(true, nil, cmd, args...)
}

// outCommand runs the specified command and returns any standard output
// as well as any errors.
func outCommand(cmd string, args ...string) ([]byte, error) {
	return runCommand(false, nil, cmd, args...)
}

func checkDependencies() error {
	var errs errlist.List
	for _, bin := range []string{"docker", "kubectl", "kind"} {
		if _, err := execLookPath(bin); err != nil {
			errs.Add(fmt.Errorf("install dependency %q to configure docker in kind", bin))
		}
	}
	return errs.Err()
}

func CreateKindDockerConfigWithGAR(ctx context.Context, registries []string) error {
	if err := checkDependencies(); err != nil {
		return err
	}
	if b, err := outCommand("kubectl", "config", "current-context"); err != nil {
		return err
	}
	clusterName := string(b)
	if !strings.HasPrefix(clusterName, "kind-") {
		return fmt.Errorf("current cluster %v is not a kind cluster", clusterName)
	}
	parts := strings.SplitN(clusterName, "-")
	if len(parts) != 2 {
		return fmt.Errorf("unable to parse kind cluster name")
	}
	kindName := parts[1]
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
		if err := logCommandWithInput([]byte(token.AccessToken), "docker", "login", "-u", "oauth2accesstoken", "--password-stdin", s); err != nil {
			return err
		}
	}
	args := []string{"get", "nodes"}
	if kindName != "" {
		args = append(args, "--name", kindName)
	}
	nodes, err := outCommand("kind", args...)
	if err != nil {
		return err
	}
	// Copy the new docker config to each node and restart kubelet so it
	// picks up the new config that contains the embedded credentials.
	for _, node := range strings.Split(string(nodes), " ") {
		node = strings.TrimSuffix(node, "\n")
		if err := logCommand("docker", "cp", configPath, fmt.Sprintf(kubeletConfigPathTemplate, node)); err != nil {
			return err
		}
		if err := logCommand("docker", "exec", node, "systemctl", "restart", "kubelet.service"); err != nil {
			return err
		}
	}
	log.Infof("Setup credentials for accessing GAR locations %v in kind cluster", registries)
	return nil
}

func RefreshKindDockerConfigWithGAR(ctx context.Context) error {
	if err := checkDependencies(); err != nil {
		return err
	}
	args := []string{"get", "nodes"}
	if k.Name != "" {
		args = append(args, "--name", k.Name)
	}
	nodes, err := outCommand("kind", args...)
	if err != nil {
		return err
	}
	for _, node := range strings.Split(string(nodes), " ") {
		node = strings.TrimSuffix(node, "\n")
		if cfg, err := outCommand("docker", "cat", fmt.Sprintf(kubeletConfigPathTemplate, node)); err != nil {
			return err
		}
		var dCfg DockerConfig{}
		if err := json.Unmarshal(cfg, &dCfg); err != nil {
			return err
		}
		var registries []string
		for r := range dCfg.Auths {
			registries = append(registries, r)
		}
		log.Infof("Refreshing for regs: %v", registries)
		return CreateKindDockerConfigWithGAR(ctx, registries)
	}
	log.Infof("Nothing to refresh")
	return nil
}

func currentKindClusterName() (string, error) {
	if b, err := outCommand("kubectl", "config", "current-context"); err != nil {
		return "", fmt.Errorf("unable to determine current cluster information: %v", err)
	}
	clusterName := string(b)
	if !strings.HasPrefix(clusterName, "kind-") {
		return "", fmt.Errorf("current cluster %v is not a kind cluster", clusterName)
	}
	parts := strings.SplitN(clusterName, "-")
	if len(parts) != 2 {
		return "", fmt.Errorf("unable to parse kind cluster name")
	}
	return parts[1], nil
}

func listKindNodes(ctx context.Context) ([]string, error) {
	args := []string{"get", "nodes"}
	if k.Name != "" {
		args = append(args, "--name", k.Name)
	}
	nodes, err := outCommand("kind", args...)
	if err != nil {
		return err
	}
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
