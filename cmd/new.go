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

package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/pulumi/pulumi/pkg/backend"
	"github.com/pulumi/pulumi/pkg/resource/config"
	"github.com/pulumi/pulumi/pkg/tokens"
	"github.com/pulumi/pulumi/pkg/workspace"

	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/pkg/diag/colors"

	"github.com/pulumi/pulumi/pkg/util/cmdutil"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/spf13/cobra"

	survey "gopkg.in/AlecAivazis/survey.v1"
	surveycore "gopkg.in/AlecAivazis/survey.v1/core"
)

func newNewCmd() *cobra.Command {
	var cloudURL string
	var name string
	var description string
	var force bool
	var yes bool
	var offline bool
	var generateOnly bool
	var dir string

	cmd := &cobra.Command{
		Use:   "new [template]",
		Short: "Create a new Pulumi project",
		Args:  cmdutil.MaximumNArgs(1),
		Run: cmdutil.RunFunc(func(cmd *cobra.Command, args []string) error {
			var err error

			// Validate name (if specified) before further prompts/operations.
			if name != "" && !workspace.IsValidProjectName(name) {
				return errors.Errorf("'%s' is not a valid project name", name)
			}

			displayOpts := backend.DisplayOptions{
				Color: cmdutil.GetGlobalColorization(),
			}

			// Get the current working directory.
			var cwd string
			if cwd, err = os.Getwd(); err != nil {
				return errors.Wrap(err, "getting the working directory")
			}
			originalCwd := cwd

			// If dir was specified, ensure it exists and use it as the
			// current working directory.
			if dir != "" {
				// Ensure the directory exists.
				if err = os.MkdirAll(dir, os.ModePerm); err != nil {
					return errors.Wrap(err, "creating the directory")
				}

				// Change the working directory to the specified directory.
				if err = os.Chdir(dir); err != nil {
					return errors.Wrap(err, "changing the working directory")
				}

				// Get the new working directory.
				if cwd, err = os.Getwd(); err != nil {
					return errors.Wrap(err, "getting the working directory")
				}
			}

			// If we're going to be creating a stack, get the current backend, which
			// will kick off the login flow (if not already logged-in).
			var b backend.Backend
			if !generateOnly {
				b, err = currentBackend(displayOpts)
				if err != nil {
					return err
				}
			}

			// Get the selected template.
			var templateName string
			if len(args) > 0 {
				templateName = strings.ToLower(args[0])
			} else {
				if templateName, err = chooseTemplate(offline, displayOpts); err != nil {
					return err
				}
			}

			var template workspace.Template

			if strings.HasPrefix(templateName, "https://") {
				if offline {
					return errors.Errorf("cannot use %s offline", templateName)
				}

				// Create a temp dir.
				var temp string
				if temp, err = ioutil.TempDir("", "pulumi-template"); err != nil {
					return err
				}
				defer contract.IgnoreError(os.RemoveAll(temp))

				var fullPath string
				if fullPath, err = workspace.RetrieveTemplate(templateName, temp); err != nil {
					return err
				}

				// Load the template.
				template, err = workspace.LoadTemplate(fullPath)
				if err != nil {
					return err
				}
			} else {
				// Download and install the template to the local template cache.
				if !offline {
					// Clone or Update the templates.
					// TODO only do the CloneOrUpdatePulumiTemplates operation once. Right now we're doing it for both
					// chooseTemplate and here.
					if err = workspace.CloneOrUpdatePulumiTemplates(); err != nil {
						message := ""
						// If the local template is available locally, provide a nicer error message.
						if localTemplates, localErr := workspace.ListLocalTemplates(); localErr == nil && len(localTemplates) > 0 {
							_, m := templateArrayToStringArrayAndMap(localTemplates)
							if _, ok := m[templateName]; ok {
								message = fmt.Sprintf(
									"; rerun the command and pass --offline to use locally cached template '%s'",
									templateName)
							}
						}

						return errors.Wrapf(err, "cloning templates%s", message)
					}
				}

				// Load the local template.
				if template, err = workspace.LoadLocalTemplate(templateName); err != nil {
					return errors.Wrapf(err, "template '%s' not found", templateName)
				}
			}

			// Do a dry run, if we're not forcing files to be overwritten.
			if !force {
				if err = template.CopyTemplateFilesDryRun(cwd); err != nil {
					if os.IsNotExist(err) {
						return errors.Wrapf(err, "template '%s' not found", templateName)
					}
					return err
				}
			}

			// Show instructions, if we're going to show at least one prompt.
			hasAtLeastOnePrompt := (name == "") || (description == "") || !generateOnly
			if !yes && hasAtLeastOnePrompt {
				fmt.Println("This command will walk you through creating a new Pulumi project.")
				fmt.Println()
				fmt.Println("Enter a value or leave blank to accept the default, and press <ENTER>.")
				fmt.Println("Press ^C at any time to quit.")
			}

			// Prompt for the project name, if it wasn't already specified.
			if name == "" {
				defaultValue := workspace.ValueOrSanitizedDefaultProjectName(name, filepath.Base(cwd))
				name, err = promptForValue(yes, "project name", defaultValue, false, workspace.IsValidProjectName, displayOpts)
				if err != nil {
					return err
				}
			}

			// Prompt for the project description, if it wasn't already specified.
			if description == "" {
				defaultValue := workspace.ValueOrDefaultProjectDescription(description, template.Description)
				description, err = promptForValue(yes, "project description", defaultValue, false, nil, displayOpts)
				if err != nil {
					return err
				}
			}

			// Actually copy the files.
			if err = template.CopyTemplateFiles(cwd, force, name, description); err != nil {
				if os.IsNotExist(err) {
					return errors.Wrapf(err, "template '%s' not found", templateName)
				}
				return err
			}

			fmt.Printf("Created project '%s'.\n", name)

			// Prompt for the stack name and create the stack.
			var stack backend.Stack
			if !generateOnly {
				defaultValue := getDevStackName(name)

				for {
					var stackName string
					stackName, err = promptForValue(yes, "stack name", defaultValue, false, nil, displayOpts)
					if err != nil {
						return err
					}
					stack, err = stackInit(b, stackName)
					if err != nil {
						if !yes {
							// Let the user know about the error and loop around to try again.
							fmt.Printf("Sorry, could not create stack '%s': %v.\n", stackName, err)
							continue
						}
						return err
					}
					break
				}

				// The backend will print "Created stack '<stack>'." on success.
			}

			// Prompt for config values and save.
			if !generateOnly {
				var keys config.KeyArray
				for k := range template.Config {
					keys = append(keys, k)
				}
				if len(keys) > 0 {
					sort.Sort(keys)

					c := make(config.Map)
					for _, k := range keys {
						// TODO show description.
						defaultValue := template.Config[k].Default
						secret := template.Config[k].Secret
						var value string
						value, err = promptForValue(yes, k.String(), defaultValue, secret, nil, displayOpts)
						if err != nil {
							return err
						}
						c[k] = config.NewValue(value)
					}

					if err = saveConfig(stack.Name().StackName(), c); err != nil {
						return errors.Wrap(err, "saving config")
					}

					fmt.Println("Saved config.")
				}
			}

			// Install dependencies.
			if !generateOnly {
				fmt.Println("Installing dependencies...")
				err = installDependencies()
				if err != nil {
					return err
				}
				fmt.Println("Finished installing dependencies.")

				// Write a summary with next steps.
				fmt.Println("New project is configured and ready to deploy.")

				// If the current working directory changed, add instructions to
				// cd into the directory.
				if originalCwd != cwd {
					// If we can determine a relative path, use that, otherwise use
					// the full path.
					var cd string
					if rel, err := filepath.Rel(originalCwd, cwd); err == nil {
						cd = rel
					} else {
						cd = cwd
					}

					// Surround the path with double quotes if it contains whitespace.
					if containsWhiteSpace(cd) {
						cd = fmt.Sprintf("\"%s\"", cd)
					}

					fmt.Printf("Run 'cd %s' then 'pulumi update'.\n", cd)
				} else {
					fmt.Println("Run 'pulumi update'.")
				}
			}

			return nil
		}),
	}

	cmd.PersistentFlags().StringVarP(&cloudURL,
		"cloud-url", "c", "", "A cloud URL to download templates from")
	cmd.PersistentFlags().StringVarP(
		&name, "name", "n", "",
		"The project name; if not specified, a prompt will request it")
	cmd.PersistentFlags().StringVarP(
		&description, "description", "d", "",
		"The project description; if not specified, a prompt will request it")
	cmd.PersistentFlags().BoolVarP(
		&force, "force", "f", false,
		"Forces content to be generated even if it would change existing files")
	cmd.PersistentFlags().BoolVarP(
		&yes, "yes", "y", false,
		"Skip prompts and proceed with default values")
	cmd.PersistentFlags().BoolVarP(
		&offline, "offline", "o", false,
		"Use locally cached templates without making any network requests")
	cmd.PersistentFlags().BoolVar(
		&generateOnly, "generate-only", false,
		"Generate the project only; do not create a stack, save config, or install dependencies")
	cmd.PersistentFlags().StringVar(&dir, "dir", "",
		"The location to place the generated project; if not specified, the current directory is used")

	return cmd
}

// getDevStackName returns the stack name suffixed with -dev.
func getDevStackName(name string) string {
	const suffix = "-dev"
	// Strip the suffix so we don't include two -dev suffixes
	// if the name already has it.
	return strings.TrimSuffix(name, suffix) + suffix
}

// stackInit creates the stack.
func stackInit(b backend.Backend, stackName string) (backend.Stack, error) {
	stackRef, err := b.ParseStackReference(stackName)
	if err != nil {
		return nil, err
	}
	return createStack(b, stackRef, nil)
}

// saveConfig saves the config for the stack.
func saveConfig(stackName tokens.QName, c config.Map) error {
	ps, err := workspace.DetectProjectStack(stackName)
	if err != nil {
		return err
	}

	for k, v := range c {
		ps.Config[k] = v
	}

	return workspace.SaveProjectStack(stackName, ps)
}

// installDependencies will install dependencies for the project, e.g. by running
// `npm install` for nodejs projects or `pip install` for python projects.
func installDependencies() error {
	proj, _, err := readProject()
	if err != nil {
		return err
	}

	// TODO[pulumi/pulumi#1307]: move to the language plugins so we don't have to hard code here.
	var command string
	var c *exec.Cmd
	if strings.EqualFold(proj.Runtime, "nodejs") {
		command = "npm install"
		c = exec.Command("npm", "install") // nolint: gas, intentionally launching with partial path
	} else if strings.EqualFold(proj.Runtime, "python") {
		command = "pip install -r requirements.txt"
		c = exec.Command("pip", "install", "-r", "requirements.txt") // nolint: gas, intentionally launching with partial path
	} else {
		return nil
	}

	// Run the command.
	if out, err := c.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "%s", out)
		return errors.Wrapf(err, "installing dependencies; rerun '%s' manually to try again", command)
	}

	return nil
}

// chooseTemplate will prompt the user to choose amongst the available templates.
func chooseTemplate(offline bool, opts backend.DisplayOptions) (string, error) {
	const chooseTemplateErr = "no template selected; please use `pulumi new` to choose one"
	if !cmdutil.Interactive() {
		return "", errors.New(chooseTemplateErr)
	}

	var templates []workspace.Template
	var err error

	if !offline {
		if templates, err = workspace.ListTemplates(); err != nil {
			message := "could not fetch list of remote templates"

			// If we couldn't fetch the list, see if there are any local templates
			if localTemplates, localErr := workspace.ListLocalTemplates(); localErr == nil && len(localTemplates) > 0 {
				options, _ := templateArrayToStringArrayAndMap(localTemplates)
				message = message + "\nrerun the command and pass --offline to use locally cached templates: " +
					strings.Join(options, ", ")
			}

			return "", errors.Wrap(err, message)
		}
	} else {
		if templates, err = workspace.ListLocalTemplates(); err != nil || len(templates) == 0 {
			return "", errors.Wrap(err, chooseTemplateErr)
		}
	}

	// Customize the prompt a little bit (and disable color since it doesn't match our scheme).
	surveycore.DisableColor = true
	surveycore.QuestionIcon = ""
	surveycore.SelectFocusIcon = opts.Color.Colorize(colors.BrightGreen + ">" + colors.Reset)
	message := "\rPlease choose a template:"
	message = opts.Color.Colorize(colors.BrightWhite + message + colors.Reset)

	options, _ := templateArrayToStringArrayAndMap(templates)

	var option string
	if err := survey.AskOne(&survey.Select{
		Message:  message,
		Options:  options,
		PageSize: len(options),
	}, &option, nil); err != nil {
		return "", errors.New(chooseTemplateErr)
	}

	return option, nil
}

// promptForValue prompts the user for a value with a defaultValue preselected. Hitting enter accepts the
// default. If yes is true, defaultValue is returned without prompting. isValidFn is an optional parameter;
// when specified, it will be run to validate that value entered. An invalid value will result in an error
// message followed by another prompt for the value.
func promptForValue(
	yes bool, prompt string, defaultValue string, secret bool,
	isValidFn func(value string) bool, opts backend.DisplayOptions) (string, error) {

	if yes {
		return defaultValue, nil
	}

	for {
		if defaultValue == "" {
			prompt = opts.Color.Colorize(
				fmt.Sprintf("%s%s:%s ", colors.BrightCyan, prompt, colors.Reset))
		} else {
			prompt = opts.Color.Colorize(
				fmt.Sprintf("%s%s: (%s)%s ", colors.BrightCyan, prompt, defaultValue, colors.Reset))
		}
		fmt.Print(prompt)

		// Read the value.
		var err error
		var value string
		if secret {
			value, err = cmdutil.ReadConsoleNoEcho("")
			if err != nil {
				return "", err
			}
		} else {
			value, err = cmdutil.ReadConsole("")
			if err != nil {
				return "", err
			}
		}
		value = strings.TrimSpace(value)

		if value != "" {
			if isValidFn == nil || isValidFn(value) {
				return value, nil
			}

			// The value is invalid, let the user know and try again
			fmt.Printf("Sorry, '%s' is not a valid %s.\n", value, prompt)
			continue
		}
		return defaultValue, nil
	}
}

// templateArrayToStringArrayAndMap returns an array of template names and map of names to templates
// from an array of templates.
func templateArrayToStringArrayAndMap(templates []workspace.Template) ([]string, map[string]workspace.Template) {
	var options []string
	nameToTemplateMap := make(map[string]workspace.Template)
	for _, template := range templates {
		options = append(options, template.Name)
		nameToTemplateMap[template.Name] = template
	}
	sort.Strings(options)

	return options, nameToTemplateMap
}

// containsWhiteSpace returns true if the string contains whitespace.
func containsWhiteSpace(value string) bool {
	for _, c := range value {
		if unicode.IsSpace(c) {
			return true
		}
	}
	return false
}
