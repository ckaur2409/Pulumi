// Copyright 2016-2018, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gitutil

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/util/fsutil"
	git "gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

// GetGitRepository returns the git repository by walking up from the provided directory.
// If no repository is found, will return (nil, nil).
func GetGitRepository(dir string) (*git.Repository, error) {
	gitRoot, err := fsutil.WalkUp(dir, func(s string) bool { return filepath.Base(s) == ".git" }, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "searching for git repository from %v", dir)
	}
	if gitRoot == "" {
		return nil, nil
	}

	// Open the git repo in the .git folder's parent, not the .git folder itself.
	repo, err := git.PlainOpen(path.Join(gitRoot, ".."))
	if err == git.ErrRepositoryNotExists {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "reading git repository")
	}
	return repo, nil
}

// GetGitHubProjectForOrigin returns the GitHub login, and GitHub repo name if the "origin" remote is
// a GitHub URL.
func GetGitHubProjectForOrigin(dir string) (string, string, error) {
	repo, err := GetGitRepository(dir)
	if repo == nil {
		return "", "", fmt.Errorf("no git repository found from %v", dir)
	}
	if err != nil {
		return "", "", err
	}
	return GetGitHubProjectForOriginByRepo(repo)
}

// GetGitHubProjectForOriginByRepo returns the GitHub login, and GitHub repo name if the "origin" remote is
// a GitHub URL.
func GetGitHubProjectForOriginByRepo(repo *git.Repository) (string, string, error) {
	remote, err := repo.Remote("origin")
	if err != nil {
		return "", "", errors.Wrap(err, "could not read origin information")
	}

	remoteURL := ""
	if len(remote.Config().URLs) > 0 {
		remoteURL = remote.Config().URLs[0]
	}
	project := ""

	const GitHubSSHPrefix = "git@github.com:"
	const GitHubHTTPSPrefix = "https://github.com/"
	const GitHubRepositorySuffix = ".git"

	if strings.HasPrefix(remoteURL, GitHubSSHPrefix) {
		project = trimGitRemoteURL(remoteURL, GitHubSSHPrefix, GitHubRepositorySuffix)
	} else if strings.HasPrefix(remoteURL, GitHubHTTPSPrefix) {
		project = trimGitRemoteURL(remoteURL, GitHubHTTPSPrefix, GitHubRepositorySuffix)
	}

	split := strings.Split(project, "/")

	if len(split) != 2 {
		return "", "", errors.Errorf("could not detect GitHub project from url: %v", remote)
	}

	return split[0], split[1], nil
}

// GitCloneOrPull clones or updates a Git repository.
func GitCloneOrPull(url string, path string) (*git.Repository, error) {
	var repo *git.Repository
	var err error

	// Attempt to clone the repo.
	cloneOpts := &git.CloneOptions{
		URL:   url,
		Depth: 1,
	}
	repo, err = git.PlainClone(path, false, cloneOpts)
	if err != nil {
		// If the repo already exists, open it and pull.
		if err == git.ErrRepositoryAlreadyExists {
			repo, err = git.PlainOpen(path)
			if err != nil {
				return nil, err
			}

			var w *git.Worktree
			if w, err = repo.Worktree(); err != nil {
				return nil, err
			}

			pullOpts := &git.PullOptions{
				Depth: 1,
				Force: true,
			}
			if err = w.Pull(pullOpts); err != nil && err != git.NoErrAlreadyUpToDate {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return repo, nil
}

// ParseGitRepoURL returns the URL to the Git repository from a raw URL.
// For example, all of the following return "https://github.com/pulumi/templates.git":
//  - "https://github.com/pulumi/templates.git"
//  - "https://github.com/pulumi/templates"
//  - "https://github.com/pulumi/templates/"
//  - "https://github.com/pulumi/templates/templates"
//  - "https://github.com/pulumi/templates/templates/typescript"
//  - "https://github.com/pulumi/templates/tree/master/templates/typescript"
func ParseGitRepoURL(rawurl string) (string, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return "", err
	}

	if u.Scheme != "https" {
		return "", errors.New("invalid URL scheme")
	}

	paths := strings.Split(u.Path, "/")
	if len(paths) < 3 {
		return "", errors.New("invalid Git URL")
	}

	owner := paths[1]
	if owner == "" {
		return "", errors.New("invalid Git URL; no owner")
	}

	repo := paths[2]
	if repo == "" {
		return "", errors.New("invalid Git URL; no repository")
	}

	if !strings.HasSuffix(repo, ".git") {
		repo = repo + ".git"
	}

	return u.Scheme + "://" + u.Host + "/" + owner + "/" + repo, nil
}

var gitSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// ParseGitCheckoutOptionsSubDirectory returns the checkout options and sub directory path.
// The sub directory path always uses "/" as the separator.
func ParseGitCheckoutOptionsSubDirectory(rawurl string, repo *git.Repository) (*git.CheckoutOptions, string, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, "", err
	}

	// rawurl must have owner and repo components.
	paths := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(paths) < 3 {
		return nil, "", errors.New("invalid Git URL")
	}

	// If it only has an owner and repo, use master.
	if len(paths) == 3 {
		return &git.CheckoutOptions{Branch: plumbing.Master}, "", nil
	}

	// Slice away the owner and repo, to make further comparisons simpler.
	paths = paths[3:]

	// Ensure the remaining path components are not "." or "..".
	for _, path := range paths {
		if path == "." || path == ".." {
			return nil, "", errors.New("invalid Git URL")
		}
	}

	if paths[0] == "tree" {
		if len(paths) >= 2 {
			// If it looks like a SHA, use that.
			if gitSHARegex.MatchString(paths[1]) {
				return &git.CheckoutOptions{Hash: plumbing.NewHash(paths[1])}, strings.Join(paths[2:], "/"), nil
			}

			// Otherwise, try matching based on the repo's refs.

			// Get the list of refs sorted by length.
			refs, err := listGitReferencesByShortNameLength(repo)
			if err != nil {
				return nil, "", err
			}

			// Try to find the matching ref, checking the longest names first, so
			// if there are multiple refs that would match, we pick the longest.
			path := strings.Join(paths[1:], "/") + "/"
			for _, ref := range refs {
				shortNameWithoutOrigin := strings.TrimPrefix(ref.Short(), "origin/")
				prefix := shortNameWithoutOrigin + "/"
				if strings.HasPrefix(path, prefix) {
					subDir := strings.TrimPrefix(path, prefix)
					return &git.CheckoutOptions{Branch: ref}, strings.TrimSuffix(subDir, "/"), nil
				}
			}
		}

		// If there aren't any path components after "tree", it's an error.
		return nil, "", errors.New("invalid Git URL")
	}

	// If there wasn't "tree" in the path, just use master.
	return &git.CheckoutOptions{Branch: plumbing.Master}, strings.Join(paths, "/"), nil
}

func listGitReferencesByShortNameLength(repo *git.Repository) ([]plumbing.ReferenceName, error) {
	refs, err := repo.References()
	if err != nil {
		return nil, err
	}

	var results []plumbing.ReferenceName
	err = refs.ForEach(func(ref *plumbing.Reference) error {
		results = append(results, ref.Name())
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Sort(byShortNameLengthDesc(results))

	return results, nil
}

func trimGitRemoteURL(url string, prefix string, suffix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(url, prefix), suffix)
}

type byShortNameLengthDesc []plumbing.ReferenceName

func (r byShortNameLengthDesc) Len() int      { return len(r) }
func (r byShortNameLengthDesc) Swap(i, j int) { r[i], r[j] = r[j], r[i] }
func (r byShortNameLengthDesc) Less(i, j int) bool {
	return len(r[j].Short()) < len(r[i].Short())
}
