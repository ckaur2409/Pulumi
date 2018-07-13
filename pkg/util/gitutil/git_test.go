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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	ptesting "github.com/pulumi/pulumi/pkg/testing"
)

func TestParseGitRepoURL(t *testing.T) {
	test := func(expected string, rawurl string) {
		actual, err := ParseGitRepoURL(rawurl)
		assert.NoError(t, err)
		assert.Equal(t, expected, actual)
	}

	exp := "https://github.com/pulumi/templates.git"
	test(exp, "https://github.com/pulumi/templates.git")
	test(exp, "https://github.com/pulumi/templates")
	test(exp, "https://github.com/pulumi/templates/")
	test(exp, "https://github.com/pulumi/templates/templates")
	test(exp, "https://github.com/pulumi/templates/templates/")
	test(exp, "https://github.com/pulumi/templates/tree/master/templates")
	test(exp, "https://github.com/pulumi/templates/tree/master/templates/python")
	test(exp, "https://github.com/pulumi/templates/tree/929b6e4c5c39196ae2482b318f145e0d765e9608/templates")
	test(exp, "https://github.com/pulumi/templates/tree/929b6e4c5c39196ae2482b318f145e0d765e9608/templates/python")

	testError := func(rawurl string) {
		_, err := ParseGitRepoURL(rawurl)
		assert.Error(t, err)
	}

	testError("https://github.com")
	testError("https://github.com/pulumi")
	testError("https://github.com/pulumi/")

	testError("http://github.com/pulumi/templates.git")
	testError("http://github.com/pulumi/templates")
}

func TestParseGitCheckoutOptionsSubDirectory(t *testing.T) {
	e := ptesting.NewEnvironment(t)
	defer deleteIfNotFailed(e)

	// Create local test repository.
	repoPath := filepath.Join(e.RootPath, "repo")
	err := os.MkdirAll(repoPath, os.ModePerm)
	assert.NoError(e, err, "making repo dir %s", repoPath)
	e.CWD = repoPath
	createTestRepo(e)

	// Create temp directory to clone to.
	cloneDir := filepath.Join(e.RootPath, "temp")
	err = os.MkdirAll(cloneDir, os.ModePerm)
	assert.NoError(e, err, "making clone dir %s", cloneDir)

	// Clone to temp dir.
	repo, cloneErr := GitCloneOrPull(repoPath, cloneDir)
	assert.NoError(t, cloneErr)

	test := func(expectedHashOrBranch string, expectedSubDirectory string, rawurl string) {
		opt, subDirectory, err := ParseGitCheckoutOptionsSubDirectory(rawurl, repo)
		assert.NoError(t, err)

		if opt.Branch != "" {
			assert.True(t, opt.Hash.IsZero())
			shortNameWithoutOrigin := strings.TrimPrefix(opt.Branch.Short(), "origin/")
			assert.Equal(t, expectedHashOrBranch, shortNameWithoutOrigin)
		} else {
			assert.False(t, opt.Hash.IsZero())
			assert.Equal(t, expectedHashOrBranch, opt.Hash.String())
		}

		assert.Equal(t, expectedSubDirectory, subDirectory)
	}

	const fakeURLPrefix = "https://fakegithost.com/fakeowner/fakerepo"

	// No ref or path.
	test("master", "", fakeURLPrefix+".git")
	test("master", "", fakeURLPrefix)
	test("master", "", fakeURLPrefix+"/")

	// No "tree" path component.
	test("master", "foo", fakeURLPrefix+"/foo")
	test("master", "foo", fakeURLPrefix+"/foo/")
	test("master", "content/foo", fakeURLPrefix+"/content/foo")
	test("master", "content/foo", fakeURLPrefix+"/content/foo/")

	// master.
	test("master", "", fakeURLPrefix+"/tree/master")
	test("master", "", fakeURLPrefix+"/tree/master/")
	test("master", "foo", fakeURLPrefix+"/tree/master/foo")
	test("master", "foo", fakeURLPrefix+"/tree/master/foo/")
	test("master", "content/foo", fakeURLPrefix+"/tree/master/content/foo")
	test("master", "content/foo", fakeURLPrefix+"/tree/master/content/foo/")

	// HEAD.
	test("HEAD", "", fakeURLPrefix+"/tree/HEAD")
	test("HEAD", "", fakeURLPrefix+"/tree/HEAD/")
	test("HEAD", "foo", fakeURLPrefix+"/tree/HEAD/foo")
	test("HEAD", "foo", fakeURLPrefix+"/tree/HEAD/foo/")
	test("HEAD", "content/foo", fakeURLPrefix+"/tree/HEAD/content/foo")
	test("HEAD", "content/foo", fakeURLPrefix+"/tree/HEAD/content/foo/")

	// Tag.
	test("my", "", fakeURLPrefix+"/tree/my")
	test("my", "", fakeURLPrefix+"/tree/my/")
	test("my", "foo", fakeURLPrefix+"/tree/my/foo")
	test("my", "foo", fakeURLPrefix+"/tree/my/foo/")

	// Commit SHA.
	test("2ba6921f3163493809bcbb0ec7283a0446048076", "",
		fakeURLPrefix+"/tree/2ba6921f3163493809bcbb0ec7283a0446048076")
	test("2ba6921f3163493809bcbb0ec7283a0446048076", "",
		fakeURLPrefix+"/tree/2ba6921f3163493809bcbb0ec7283a0446048076/")
	test("2ba6921f3163493809bcbb0ec7283a0446048076", "foo",
		fakeURLPrefix+"/tree/2ba6921f3163493809bcbb0ec7283a0446048076/foo")
	test("2ba6921f3163493809bcbb0ec7283a0446048076", "foo",
		fakeURLPrefix+"/tree/2ba6921f3163493809bcbb0ec7283a0446048076/foo/")
	test("2ba6921f3163493809bcbb0ec7283a0446048076", "content/foo",
		fakeURLPrefix+"/tree/2ba6921f3163493809bcbb0ec7283a0446048076/content/foo")
	test("2ba6921f3163493809bcbb0ec7283a0446048076", "content/foo",
		fakeURLPrefix+"/tree/2ba6921f3163493809bcbb0ec7283a0446048076/content/foo/")

	// The longest ref is matched, so we should get "my/content" as the expected ref
	// and "foo" as the path (instead of "my" and "content/foo").
	test("my/content", "foo", fakeURLPrefix+"/tree/my/content/foo")
	test("my/content", "foo", fakeURLPrefix+"/tree/my/content/foo/")

	testError := func(rawurl string) {
		_, _, err := ParseGitCheckoutOptionsSubDirectory(rawurl, repo)
		assert.Error(t, err)
	}

	// No ref specified.
	testError(fakeURLPrefix + "/tree")
	testError(fakeURLPrefix + "/tree/")

	// Invalid casing.
	testError(fakeURLPrefix + "/tree/Master")
	testError(fakeURLPrefix + "/tree/head")
	testError(fakeURLPrefix + "/tree/My")

	// Path components cannot contain "." or "..".
	testError(fakeURLPrefix + "/.")
	testError(fakeURLPrefix + "/./")
	testError(fakeURLPrefix + "/..")
	testError(fakeURLPrefix + "/../")
	testError(fakeURLPrefix + "/foo/.")
	testError(fakeURLPrefix + "/foo/./")
	testError(fakeURLPrefix + "/foo/..")
	testError(fakeURLPrefix + "/foo/../")
	testError(fakeURLPrefix + "/content/./foo")
	testError(fakeURLPrefix + "/content/./foo/")
	testError(fakeURLPrefix + "/content/../foo")
	testError(fakeURLPrefix + "/content/../foo/")
}

func createTestRepo(e *ptesting.Environment) {
	e.RunCommand("git", "init")

	writeTestFile(e, "README.md", "test repo")
	e.RunCommand("git", "add", "*")
	e.RunCommand("git", "commit", "-m", "'Initial commit'")

	writeTestFile(e, "foo/bar.md", "foo-bar.md")
	e.RunCommand("git", "add", "*")
	e.RunCommand("git", "commit", "-m", "'foo dir'")

	writeTestFile(e, "content/foo/bar.md", "content-foo-bar.md")
	e.RunCommand("git", "add", "*")
	e.RunCommand("git", "commit", "-m", "'content-foo dir'")

	e.RunCommand("git", "branch", "my/content")
	e.RunCommand("git", "tag", "my")
}

func writeTestFile(e *ptesting.Environment, filename string, contents string) {
	filename = filepath.Join(e.CWD, filename)

	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, os.ModePerm)
	assert.NoError(e, err, "making all directories %s", dir)

	err = ioutil.WriteFile(filename, []byte(contents), os.ModePerm)
	assert.NoError(e, err, "writing %s file", filename)
}

// deleteIfNotFailed deletes the files in the testing environment if the testcase has
// not failed. (Otherwise they are left to aid debugging.)
func deleteIfNotFailed(e *ptesting.Environment) {
	if !e.T.Failed() {
		e.DeleteEnvironment()
	}
}
