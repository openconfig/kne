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

// Package release provides subcommands for releasing KNE artifacts.
package release

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	cloudbuild "cloud.google.com/go/cloudbuild/apiv1/v2"
	"cloud.google.com/go/cloudbuild/apiv1/v2/cloudbuildpb"
	"github.com/spf13/cobra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// New returns the release subcommand.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use: "release",
	}
	cmd.AddCommand(meshnet())
	cmd.AddCommand(bridge())
	return cmd
}

// bridge releases the packet bridge image. The image is the kne binary with
// `kne bridge` as its entrypoint, so it has no version of its own: releasing it
// tags KNE as a whole and labels the image with that version.
func bridge() *cobra.Command {
	return &cobra.Command{
		Use:   "bridge <version>",
		Short: "Release the bridge image, tagging KNE as a whole at <version>",
		Args:  cobra.ExactArgs(1),
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

			// Deliberately unprefixed, unlike meshnet: meshnet is a separate
			// vendored component with its own source tree, whereas the bridge
			// ships inside the kne binary and so shares KNE's version.
			tag := args[0]
			if err := checkTag(tag, sha); err != nil {
				return err
			}

			if err := checkOrRunPrerelease(cmd.Context(), sha); err != nil {
				return err
			}

			pushedAt := time.Now()
			pushed, err := ensureTag(tag, sha)
			if err != nil {
				return err
			}
			return ensureReleaseBuild(cmd.Context(), "bridge-release", tag, args[0], pushed, pushedAt)
		},
	}
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

			tag := fmt.Sprintf("third_party/meshnet/%s", args[0])
			if err := checkTag(tag, sha); err != nil {
				return err
			}

			if err := checkOrRunPrerelease(cmd.Context(), sha); err != nil {
				return err
			}

			pushedAt := time.Now()
			pushed, err := ensureTag(tag, sha)
			if err != nil {
				return err
			}
			return ensureReleaseBuild(cmd.Context(), "meshnet-release", tag, args[0], pushed, pushedAt)
		},
	}
}

// checkTag verifies whether tag already exists on origin or locally, and returns an error
// if it exists pointing to a commit other than expectedSHA.
func checkTag(tag, expectedSHA string) error {
	remoteOut, err := exec.Command("git", "ls-remote", "origin", fmt.Sprintf("refs/tags/%s*", tag)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check remote tag on origin: out %s, error %v", string(remoteOut), err)
	}
	remoteSHA := parseLsRemoteTagSHA(string(remoteOut), tag)
	if remoteSHA != "" && remoteSHA != expectedSHA {
		return fmt.Errorf("tag %q already exists on origin pointing to %s, but expected commit %s", tag, remoteSHA, expectedSHA)
	}

	localOut, err := exec.Command("git", "rev-parse", "-q", "--verify", fmt.Sprintf("refs/tags/%s^{commit}", tag)).CombinedOutput()
	localSHA := strings.TrimSpace(string(localOut))
	if err == nil && localSHA != "" && localSHA != expectedSHA {
		return fmt.Errorf("tag %q already exists locally pointing to %s, but expected commit %s", tag, localSHA, expectedSHA)
	}
	return nil
}

// parseLsRemoteTagSHA extracts the commit SHA for target tag from git ls-remote output,
// peeling annotated tags if ^{} is present.
func parseLsRemoteTagSHA(output, tag string) string {
	targetRef := fmt.Sprintf("refs/tags/%s", tag)
	peeledRef := targetRef + "^{}"
	var tagSHA, peeledSHA string
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) >= 2 {
			switch parts[1] {
			case peeledRef:
				peeledSHA = parts[0]
			case targetRef:
				tagSHA = parts[0]
			}
		}
	}
	if peeledSHA != "" {
		return peeledSHA
	}
	return tagSHA
}

// ensureTag creates the tag locally (if needed) and pushes it to origin (if needed).
// It returns true if the tag was pushed to origin in this call, or false if it was already on origin.
func ensureTag(tag, expectedSHA string) (bool, error) {
	if err := checkTag(tag, expectedSHA); err != nil {
		return false, err
	}

	remoteOut, err := exec.Command("git", "ls-remote", "origin", fmt.Sprintf("refs/tags/%s*", tag)).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to check remote tag on origin: out %s, error %v", string(remoteOut), err)
	}
	remoteSHA := parseLsRemoteTagSHA(string(remoteOut), tag)

	localOut, err := exec.Command("git", "rev-parse", "-q", "--verify", fmt.Sprintf("refs/tags/%s^{commit}", tag)).CombinedOutput()
	localSHA := strings.TrimSpace(string(localOut))
	if err != nil || localSHA == "" {
		// Tag doesn't exist locally; create it
		if out, err := exec.Command("git", "tag", tag, expectedSHA).CombinedOutput(); err != nil {
			return false, fmt.Errorf("failed to create tag %q at %s: out %s, error %v", tag, expectedSHA, string(out), err)
		}
	}

	if remoteSHA == expectedSHA {
		fmt.Printf("Tag %s already exists on origin at commit %s\n", tag, expectedSHA)
		return false, nil
	}

	fmt.Println("Creating and Pushing Tag:", tag)
	if out, err := exec.Command("git", "push", "origin", tag).CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to push tag %q: out %s, error %v", tag, string(out), err)
	}
	return true, nil
}

const (
	// cloudBuildEndpoint is the regional endpoint for the cloud build API.
	cloudBuildEndpoint = "us-central1-cloudbuild.googleapis.com:443"
	quotaProjectID     = "kne-external"
	triggerNamePrefix  = "projects/kne-external/locations/us-central1/triggers"
	parentName         = "projects/kne-external/locations/us-central1"
)

func newCloudBuildClient(ctx context.Context) (*cloudbuild.Client, error) {
	return cloudbuild.NewClient(ctx, option.WithEndpoint(cloudBuildEndpoint), option.WithQuotaProject(quotaProjectID))
}

// checkOrRunPrerelease checks if prerelease tests have already passed for sha, or runs them.
func checkOrRunPrerelease(ctx context.Context, sha string) (rErr error) {
	fmt.Println("Checking prerelease tests")
	c, err := newCloudBuildClient(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil && rErr == nil {
			rErr = err
		}
	}()

	filter := fmt.Sprintf(`substitutions.COMMIT_SHA = %q AND substitutions.TRIGGER_NAME = "kne-test"`, sha)
	it := c.ListBuilds(ctx, &cloudbuildpb.ListBuildsRequest{
		Parent:   parentName,
		Filter:   filter,
		PageSize: 10,
	})
	var runningBuild *cloudbuildpb.Build
	for {
		b, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			break
		}
		if b.GetStatus() == cloudbuildpb.Build_SUCCESS {
			fmt.Printf("Prerelease tests already passed for commit %s (Build ID: %s)\n", sha, b.GetId())
			return nil
		}
		if runningBuild == nil && (b.GetStatus() == cloudbuildpb.Build_WORKING || b.GetStatus() == cloudbuildpb.Build_QUEUED || b.GetStatus() == cloudbuildpb.Build_PENDING) {
			runningBuild = b
		}
	}

	if runningBuild != nil {
		fmt.Printf("Prerelease tests already running (Build ID: %s)\nLogs: %s\n", runningBuild.GetId(), runningBuild.GetLogUrl())
		return waitForBuildCompletion(ctx, c, runningBuild.GetName())
	}

	fmt.Println("Running prerelease tests")
	return triggerBuild(ctx, "kne-test", sha, false, nil)
}

// ensureReleaseBuild checks if the release build for tag is already complete,
// waits for an in-progress or newly triggered automatic build, or triggers a build if needed.
func ensureReleaseBuild(ctx context.Context, trigger, tag, version string, tagPushed bool, pushedAt time.Time) (rErr error) {
	c, err := newCloudBuildClient(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err := c.Close(); err != nil && rErr == nil {
			rErr = err
		}
	}()

	filter := fmt.Sprintf(`substitutions.TAG_NAME = %q`, tag)

	// If tag was not pushed in this invocation, check if a successful or in-progress build already exists.
	if !tagPushed {
		it := c.ListBuilds(ctx, &cloudbuildpb.ListBuildsRequest{
			Parent:   parentName,
			Filter:   filter,
			PageSize: 5,
		})
		var runningBuild *cloudbuildpb.Build
		for {
			b, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				break
			}
			if b.GetStatus() == cloudbuildpb.Build_SUCCESS {
				fmt.Printf("Release build already complete for tag %s (Build ID: %s, Status: %s)\n", tag, b.GetId(), b.GetStatus())
				return nil
			}
			if runningBuild == nil && (b.GetStatus() == cloudbuildpb.Build_WORKING || b.GetStatus() == cloudbuildpb.Build_QUEUED || b.GetStatus() == cloudbuildpb.Build_PENDING) {
				runningBuild = b
			}
		}
		if runningBuild != nil {
			fmt.Printf("Found in-progress release build (Build ID: %s)\nLogs: %s\n", runningBuild.GetId(), runningBuild.GetLogUrl())
			return waitForBuildCompletion(ctx, c, runningBuild.GetName())
		}
		// No successful or running build exists for this tag; trigger one manually.
		fmt.Printf("No successful or running build found for tag %s; triggering build\n", tag)
		return triggerBuild(ctx, trigger, tag, true, map[string]string{
			"_IMAGE_TAG": version,
		})
	}

	// Tag was pushed in this invocation: wait for the automatic build to appear and finish.
	fmt.Println("Waiting for automatic build to start")
	var build *cloudbuildpb.Build
	pollCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for build == nil {
		it := c.ListBuilds(pollCtx, &cloudbuildpb.ListBuildsRequest{
			Parent:   parentName,
			Filter:   filter,
			PageSize: 5,
		})
		for {
			b, err := it.Next()
			if err == iterator.Done {
				break
			}
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					break
				}
				return fmt.Errorf("failed to list builds: %w", err)
			}
			if b.GetCreateTime().AsTime().After(pushedAt.Add(-1 * time.Minute)) {
				build = b
				break
			}
		}
		if build != nil {
			break
		}
		select {
		case <-pollCtx.Done():
			// Fallback: if automatic build didn't appear, trigger it manually
			fmt.Println("Automatic build did not appear within 2 minutes; triggering build manually")
			return triggerBuild(ctx, trigger, tag, true, map[string]string{
				"_IMAGE_TAG": version,
			})
		case <-time.After(2 * time.Second):
		}
	}

	fmt.Printf("Build ID: %s\nLogs: %s\n", build.GetId(), build.GetLogUrl())
	return waitForBuildCompletion(ctx, c, build.GetName())
}

func waitForBuildCompletion(ctx context.Context, c *cloudbuild.Client, buildName string) error {
	fmt.Println("Waiting for build to finish")
	for {
		b, err := c.GetBuild(ctx, &cloudbuildpb.GetBuildRequest{
			Name: buildName,
		})
		if err != nil {
			return fmt.Errorf("failed to get build status: %w", err)
		}
		switch b.GetStatus() {
		case cloudbuildpb.Build_SUCCESS:
			fmt.Println(b.GetId(), b.GetStatus())
			return nil
		case cloudbuildpb.Build_FAILURE, cloudbuildpb.Build_INTERNAL_ERROR, cloudbuildpb.Build_TIMEOUT, cloudbuildpb.Build_CANCELLED, cloudbuildpb.Build_EXPIRED:
			fmt.Println(b.GetId(), b.GetStatus())
			return fmt.Errorf("build failed with status: %s", b.GetStatus())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
}

// triggerBuild runs a cloud build trigger at the given tag if set, or the main branch if unset.
func triggerBuild(ctx context.Context, trigger, tagOrSHA string, tag bool, substitutions map[string]string) (rErr error) {
	c, err := newCloudBuildClient(ctx)
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
	if md.Build != nil {
		fmt.Printf("Build ID: %s\nLogs: %s\n", md.Build.GetId(), md.Build.GetLogUrl())
	}
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
