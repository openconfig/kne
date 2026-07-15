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

package release

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	"cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/spf13/cobra"
	"google.golang.org/api/option"
)

func NewRelease() *cobra.Command {
	cmd := &cobra.Command{
		Use: "release",
	}
	cmd.AddCommand(meshnet())
	return cmd
}

func meshnet() *cobra.Command {
	return &cobra.Command{
		Use:  "meshnet <version>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Validating working directory")
			sha, err := validateWorkDir()
			if err != nil {
				var uncleanErr *UncleanWorkDirError
				if errors.As(err, &uncleanErr) {
					for _, r := range uncleanErr.Reasons {
						fmt.Println(r)
					}
					ok, pErr := promptBool("Are you sure you want to continue")
					if pErr != nil {
						return pErr
					}
					if !ok {
						return fmt.Errorf("repository in invalid state")
					}
				} else {
					return err
				}
			}
			fmt.Println("Running prerelease tests")
			if err := triggerBuild(cmd.Context(), "kne-test", sha, false, nil); err != nil {
				return err
			}

			tag := fmt.Sprintf("third_party/meshnet/%s", args[0])
			fmt.Println("Creating and Pushing Tag:", tag)
			if err := createAndPushTag(tag); err != nil {
				return err
			}
			fmt.Println("Building and Pushing container")
			return triggerBuild(cmd.Context(), "meshnet-release", tag, true, map[string]string{
				"_IMAGE_TAG": args[0],
			})
		},
	}
}

func createAndPushTag(tag string) error {
	if out, err := exec.Command("git", "tag", tag).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create tag: out %s, error %v", string(out), err)
	}
	if out, err := exec.Command("git", "push", "origin", tag).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to push tag: out %s, error %v", string(out), err)
	}
	return nil
}

const (
	// cloudBuildEndpoint is the regional endpoint for the cloud build API.
	cloudBuildEndpoint = "us-central1-cloudbuild.googleapis.com:443"
	triggerNamePrefix  = "projects/kne-external/locations/us-central1/triggers"
)

// triggerBuild runs a cloud build trigger at the given tag if set, or the main branch if unset.
func triggerBuild(ctx context.Context, trigger, tagOrSHA string, tag bool, substitutions map[string]string) (rErr error) {
	c, err := cloudbuild.NewClient(ctx, option.WithEndpoint(cloudBuildEndpoint))
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil && rErr == nil {
			rErr = err
		}
	}()

	src := &cloudbuildpb.RepoSource{
		Revision: &cloudbuildpb.RepoSource_CommitSha{
			CommitSha: tagOrSHA,
		},
		Substitutions: substitutions,
	}
	if tag {
		src = &cloudbuildpb.RepoSource{
			Revision: &cloudbuildpb.RepoSource_TagName{
				TagName: tagOrSHA,
			},
			Substitutions: substitutions,
		}
	}

	op, err := c.RunBuildTrigger(ctx, &cloudbuildpb.RunBuildTriggerRequest{
		Name:   fmt.Sprintf("%s/%s", triggerNamePrefix, trigger),
		Source: src,
	})
	if err != nil {
		return err
	}
	if _, err := op.Poll(ctx); err != nil {
		return err
	}
	md, err := op.Metadata()
	if err != nil {
		return err
	}
	fmt.Printf("Build ID: %s\nLogs: %s\n", md.GetBuild().GetId(), md.GetBuild().GetLogUrl())
	fmt.Println("Waiting for build to finish")
	b, err := op.Wait(ctx)
	if err != nil {
		return err
	}
	fmt.Println(b.Id, b.Status)
	return nil
}

type UncleanWorkDirError struct {
	Reasons []string
}

func (e *UncleanWorkDirError) Error() string {
	return fmt.Sprintf("unclean working directory: %s", strings.Join(e.Reasons, ", "))
}

// validateWorkDir checks the status of the working dir to make sure it is clean state.
func validateWorkDir() (string, error) {
	stOut, err := exec.Command("git", "status", "--porcelain").CombinedOutput()
	if err != nil {
		return "", err
	}
	status := strings.TrimSpace(string(stOut))
	brOut, err := exec.Command("git", "branch", "--show-current").CombinedOutput()
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(brOut))
	revOut, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(revOut))
	var reasons []string
	if branch != "main" {
		reasons = append(reasons, "Not on main branch")
	}
	if status != "" {
		reasons = append(reasons, "Working directory dirty")
	}
	if len(reasons) > 0 {
		return sha, &UncleanWorkDirError{Reasons: reasons}
	}
	return sha, nil
}

// promptBool is a yes/no command line prompt.
func promptBool(prompt string) (bool, error) {
	fmt.Print(prompt + " (y/n): ")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		switch scanner.Text() {
		case "y":
			return true, nil
		case "n":
			return false, nil
		default:
			fmt.Println("invalid input")
		}
	}
	return false, scanner.Err()
}
