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

package workspace

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	git "gopkg.in/src-d/go-git.v4"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/resource/config"
	"github.com/pulumi/pulumi/pkg/tokens"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi/pkg/util/gitutil"
)

const (
	defaultProjectName = "project"

	pulumiTemplateGitRepository = "https://github.com/pulumi/templates.git"

	// This file will be ignored when copying from the template cache to
	// a project directory.
	legacyPulumiTemplateManifestFile = ".pulumi.template.yaml"
)

// Template represents a project template.
type Template struct {
	Name        string                                    // The name of the template.
	Description string                                    // Description of the template.
	Quickstart  string                                    // optional text to be displayed after template creation.
	Config      map[config.Key]ProjectTemplateConfigValue // optional template config.
}

// cleanupLegacyTemplateDir deletes the ~/.pulumi/templates directory if it isn't
// a git repository.
func cleanupLegacyTemplateDir() error {
	templateDir, err := GetTemplateDir("")
	if err != nil {
		return err
	}

	// See if the template directory is a Git repository.
	_, err = git.PlainOpen(templateDir)
	if err != nil {
		// If the repository doesn't exist, delete the entire template
		// directory and all children.
		if err == git.ErrRepositoryNotExists {
			return os.RemoveAll(templateDir)
		}

		return err
	}

	return nil
}

// CloneOrUpdatePulumiTemplates acquires/updates the Pulumi templates from
// https://github.com/pulumi/templates to ~/.pulumi/templates.
func CloneOrUpdatePulumiTemplates() error {
	// Cleanup the template directory.
	if err := cleanupLegacyTemplateDir(); err != nil {
		return err
	}

	// Get the template directory.
	templateDir, err := GetTemplateDir("")
	if err != nil {
		return err
	}

	// Ensure the template directory exists.
	if err := os.MkdirAll(templateDir, 0700); err != nil {
		return err
	}

	// Clone or update the pulumi/templates repo.
	if _, err := gitutil.GitCloneOrPull(pulumiTemplateGitRepository, templateDir); err != nil {
		return err
	}

	return nil
}

// RetrieveTemplate downloads the repo to path and returns the full path on disk.
func RetrieveTemplate(rawurl string, path string) (string, error) {
	url, err := gitutil.ParseGitRepoURL(rawurl)
	if err != nil {
		return "", err
	}

	repo, err := gitutil.GitCloneOrPull(url, path)
	if err != nil {
		return "", err
	}

	// // JVP
	// remote, err := repo.Remote("origin")
	// if err != nil {
	// 	return "", err
	// }
	// refs, err := remote.List(&git.ListOptions{})
	// if err != nil {
	// 	return "", err
	// }
	// for _, ref := range refs {
	// 	fmt.Printf("JVP: ref: %s\n", ref)
	// }

	opts, subDirectory, err := gitutil.ParseGitCheckoutOptionsSubDirectory(rawurl, repo)
	if err != nil {
		return "", err
	}

	// JVP
	fmt.Printf("JVP: Branch: %s\n", opts.Branch)

	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}

	// err = repo.Fetch(&git.FetchOptions{
	// 	RefSpecs: []gitconfig.RefSpec{"refs/*:refs/*", "HEAD:refs/heads/HEAD"},
	// })
	// if err != nil {
	// 	return "", err
	// }

	opts.Force = true
	if err = w.Checkout(opts); err != nil {
		return "", err
	}

	// Verify the sub directory exists.
	fullPath := filepath.Join(path, filepath.FromSlash(subDirectory))
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.Errorf("%s is not a directory", fullPath)
	}

	return fullPath, nil
}

// LoadLocalTemplate returns a local template.
func LoadLocalTemplate(name string) (Template, error) {
	templateDir, err := GetTemplateDir(name)
	if err != nil {
		return Template{}, err
	}

	return LoadTemplate(templateDir)
}

// LoadTemplate returns a template from a path.
func LoadTemplate(path string) (Template, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Template{}, err
	}
	if !info.IsDir() {
		return Template{}, errors.Errorf("%s is not a directory", path)
	}

	// TODO handle other extensions like Pulumi.json?
	proj, err := LoadProject(filepath.Join(path, "Pulumi.yaml"))
	if err != nil {
		return Template{}, err
	}

	template := Template{Name: filepath.Base(path)}
	if proj.Template != nil {
		template.Description = proj.Template.Description
		template.Quickstart = proj.Template.Quickstart
		template.Config = proj.Template.Config
	}

	return template, nil
}

// ListTemplates fetches and returns the list of templates.
func ListTemplates() ([]Template, error) {
	// Fetch the templates.
	if err := CloneOrUpdatePulumiTemplates(); err != nil {
		return nil, err
	}

	// Return the list of templates.
	return ListLocalTemplates()
}

// ListLocalTemplates returns a list of local templates.
func ListLocalTemplates() ([]Template, error) {
	templateDir, err := GetTemplateDir("")
	if err != nil {
		return nil, err
	}

	// Read items from ~/.pulumi/templates/templates.
	infos, err := ioutil.ReadDir(filepath.Join(templateDir, TemplateDir))
	if err != nil {
		return nil, err
	}

	var templates []Template
	for _, info := range infos {
		if info.IsDir() {
			template, err := LoadLocalTemplate(info.Name())
			if err != nil {
				return nil, err
			}
			templates = append(templates, template)
		}
	}
	return templates, nil
}

// CopyTemplateFilesDryRun does a dry run of copying a template to a destination directory,
// to ensure it won't overwrite any files.
func (template Template) CopyTemplateFilesDryRun(destDir string) error {
	var err error
	var sourceDir string
	if sourceDir, err = GetTemplateDir(template.Name); err != nil {
		return err
	}

	var existing []string
	err = walkFiles(sourceDir, destDir, func(info os.FileInfo, source string, dest string) error {
		if destInfo, statErr := os.Stat(dest); statErr == nil && !destInfo.IsDir() {
			existing = append(existing, filepath.Base(dest))
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(existing) > 0 {
		return newExistingFilesError(existing)
	}
	return nil
}

// CopyTemplateFiles does the actual copy operation to a destination directory.
func (template Template) CopyTemplateFiles(
	destDir string, force bool, projectName string, projectDescription string) error {

	sourceDir, err := GetTemplateDir(template.Name)
	if err != nil {
		return err
	}

	return walkFiles(sourceDir, destDir, func(info os.FileInfo, source string, dest string) error {
		if info.IsDir() {
			// Create the destination directory.
			return os.Mkdir(dest, 0700)
		}

		// Read the source file.
		b, err := ioutil.ReadFile(source)
		if err != nil {
			return err
		}

		// Transform only if it isn't a binary file.
		result := b
		if !isBinary(b) {
			transformed := transform(string(b), projectName, projectDescription)
			result = []byte(transformed)
		}

		// Write to the destination file.
		err = writeAllBytes(dest, result, force)
		if err != nil {
			// An existing file has shown up in between the dry run and the actual copy operation.
			if os.IsExist(err) {
				return newExistingFilesError([]string{filepath.Base(dest)})
			}
		}
		return err
	})
}

// GetTemplateDir returns the directory in which templates on the current machine are stored.
func GetTemplateDir(name string) (string, error) {
	u, err := user.Current()
	if u == nil || err != nil {
		return "", errors.Wrap(err, "getting user home directory")
	}
	dir := filepath.Join(u.HomeDir, BookkeepingDir, TemplateDir)
	if name != "" {
		dir = filepath.Join(dir, TemplateDir, name)
	}
	return dir, nil
}

// IsValidProjectName returns true if the project name is a valid name.
func IsValidProjectName(name string) bool {
	return tokens.IsPackageName(name)
}

// ValueOrSanitizedDefaultProjectName returns the value or a sanitized valid project name
// based on defaultNameToSanitize.
func ValueOrSanitizedDefaultProjectName(name string, defaultNameToSanitize string) string {
	if name != "" {
		return name
	}
	return getValidProjectName(defaultNameToSanitize)
}

// ValueOrDefaultProjectDescription returns the value or defaultDescription.
func ValueOrDefaultProjectDescription(description string, defaultDescription string) string {
	if description != "" {
		return description
	}
	if defaultDescription != "" {
		return defaultDescription
	}
	return ""
}

// getValidProjectName returns a valid project name based on the passed-in name.
func getValidProjectName(name string) string {
	// If the name is valid, return it.
	if IsValidProjectName(name) {
		return name
	}

	// Otherwise, try building-up the name, removing any invalid chars.
	var result string
	for i := 0; i < len(name); i++ {
		temp := result + string(name[i])
		if IsValidProjectName(temp) {
			result = temp
		}
	}

	// If we couldn't come up with a valid project name, fallback to a default.
	if result == "" {
		result = defaultProjectName
	}

	return result
}

// walkFiles is a helper that walks the directories/files in a source directory
// and performs an action for each item.
func walkFiles(sourceDir string, destDir string,
	actionFn func(info os.FileInfo, source string, dest string) error) error {

	contract.Require(sourceDir != "", "sourceDir")
	contract.Require(destDir != "", "destDir")
	contract.Require(actionFn != nil, "actionFn")

	infos, err := ioutil.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, info := range infos {
		name := info.Name()
		source := filepath.Join(sourceDir, name)
		dest := filepath.Join(destDir, name)

		if info.IsDir() {
			if err := actionFn(info, source, dest); err != nil {
				return err
			}

			if err := walkFiles(source, dest, actionFn); err != nil {
				return err
			}
		} else {
			// If the template has a legacy template manifest file,
			// ignore it.
			if name == legacyPulumiTemplateManifestFile {
				continue
			}

			if err := actionFn(info, source, dest); err != nil {
				return err
			}
		}
	}

	return nil
}

// newExistingFilesError returns a new error from a list of existing file names
// that would be overwritten.
func newExistingFilesError(existing []string) error {
	contract.Assert(len(existing) > 0)
	message := "creating this template will make changes to existing files:\n"
	for _, file := range existing {
		message = message + fmt.Sprintf("  overwrite   %s\n", file)
	}
	message = message + "\nrerun the command and pass --force to accept and create"
	return errors.New(message)
}

// transform returns a new string with ${PROJECT} and ${DESCRIPTION} replaced by
// the value of projectName and projectDescription.
func transform(content string, projectName string, projectDescription string) string {
	// On Windows, we need to replace \n with \r\n because go-git does not currently handle it.
	if runtime.GOOS == "windows" {
		content = strings.Replace(content, "\n", "\r\n", -1)
	}
	content = strings.Replace(content, "${PROJECT}", projectName, -1)
	content = strings.Replace(content, "${DESCRIPTION}", projectDescription, -1)
	return content
}

// writeAllBytes writes the bytes to the specified file, with an option to overwrite.
func writeAllBytes(filename string, bytes []byte, overwrite bool) error {
	flag := os.O_WRONLY | os.O_CREATE
	if overwrite {
		flag = flag | os.O_TRUNC
	} else {
		flag = flag | os.O_EXCL
	}

	f, err := os.OpenFile(filename, flag, 0600)
	if err != nil {
		return err
	}
	defer contract.IgnoreClose(f)

	_, err = f.Write(bytes)
	return err
}

// isBinary returns true if a zero byte occurs within the first
// 8000 bytes (or the entire length if shorter). This is the
// same approach that git uses to determine if a file is binary.
func isBinary(bytes []byte) bool {
	const firstFewBytes = 8000

	length := len(bytes)
	if firstFewBytes < length {
		length = firstFewBytes
	}

	for i := 0; i < length; i++ {
		if bytes[i] == 0 {
			return true
		}
	}

	return false
}
