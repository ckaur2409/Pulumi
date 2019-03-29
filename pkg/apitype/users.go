// Copyright 2016-2019, Pulumi Corporation.
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

package apitype

// UserInfo contains just the display information for a user.
type UserInfo struct {
	Name        string `json:"name"`
	GitHubLogin string `json:"githubLogin"`
	AvatarURL   string `json:"avatarUrl"`
}

// User represents a Pulumi user.
type User struct {
	ID          string `json:"id"`
	GitHubLogin string `json:"githubLogin"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	AvatarURL   string `json:"avatarUrl"`

	// Organizations is the list of Pulumi organizations the user is a member of.
	Organizations []OrganizationSummary `json:"organizations"`
	// Identities is the array of identities a Pulumi user's account is tied to.
	Identities []string `json:"identities"`
}
