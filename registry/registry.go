package registry

import (
	"golang.org/x/oauth2/google"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	googleFindDefaultCredentials = google.FindDefaultCredentials
)

// EnabledGoogleArtifactRegistries returns the list of Google Artifact Registries accessible inside the cluster.
// These are the registries available in the default namespace determined by the configured secrets. Registries
// are returned even if the associated credentials have expired.
func EnabledGoogleArtifactRegistries(ctx context.Context, kClient kubernetes.Interface) ([]string, error) {
	// check for default secrets in default namespace, return all configured registries
}

// SetupGoogleArtifactRegistryAccess configures a list of Google Artifact Registries for a specific namespace.
// Setup involves generated refreshed access tokens, storing them in k8 secrets, and making them accessible from the
// default service account for the namespace.
func SetupGoogleArtifactRegistryAccess(ctx context.Context, kClient kubernetes.Interface, ns string, registries []string) error {
	creds, err := googleFindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return fmt.Errorf("failed to find gcloud credentials: %v", err)
	}
	var secret *corev1.Secret
	if len(creds.JSON) > 0 {
		log.Infof("Default gcloud credentials contain json key, storing in secret")
		// create a secret from json for all of the registries
	} else {
		log.Infof("Default gcloud credentials contain access token, storing in secret")
		// create a secret from oauth token for all of the registries
		token, err := creds.TokenSource.Token()
		if err != nil {
			return fmt.Errorf("failed to get token from token source: %v", err)
		}
	}
	if _, err := m.kClient.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return err
	}
	// update default service account to use new secret(s)
	if _, err := m.kClient.CoreV1().ServiceAccounts(ns).Update(ctx, sa, metav1.UpdateOptions{}); err != nil {
		return err
	}
}
