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

package filestate

import (
	"context"
	"time"

	"github.com/pulumi/pulumi/pkg/apitype"
	"github.com/pulumi/pulumi/pkg/backend"
	"github.com/pulumi/pulumi/pkg/engine"
	"github.com/pulumi/pulumi/pkg/operations"
	"github.com/pulumi/pulumi/pkg/resource/config"
	"github.com/pulumi/pulumi/pkg/resource/deploy"
)

// Stack is a local stack.  This simply adds some local-specific properties atop the standard backend stack interface.
type Stack interface {
	backend.Stack
	Path() string                                   // a path to the stack's checkpoint file on disk.
	Tags() map[apitype.StackTagName]string          // the stack's tags.
	MergeTags(tags map[apitype.StackTagName]string) // merges tags with the stack's existing tags.
}

// localStack is a local stack descriptor.
type localStack struct {
	// ref is the stack's qualified name.
	ref backend.StackReference
	// path is the path to the stack's checkpoint file on disk.
	path string
	// config is this stack's config bag.
	config config.Map
	// snapshot snapshot contains the latest deployment state.
	snapshot *deploy.Snapshot
	// b is a pointer to the backend this stack belongs to.
	b *localBackend
	// tags contains metadata tags describing additional, extensible properties about this stack.
	tags map[apitype.StackTagName]string
}

func newStack(ref backend.StackReference, path string, config config.Map,
	snapshot *deploy.Snapshot, tags map[apitype.StackTagName]string, b *localBackend) Stack {
	return &localStack{
		ref:      ref,
		path:     path,
		config:   config,
		snapshot: snapshot,
		tags:     tags,
		b:        b,
	}
}

func (s *localStack) Ref() backend.StackReference                            { return s.ref }
func (s *localStack) Config() config.Map                                     { return s.config }
func (s *localStack) Snapshot(ctx context.Context) (*deploy.Snapshot, error) { return s.snapshot, nil }
func (s *localStack) Backend() backend.Backend                               { return s.b }
func (s *localStack) Path() string                                           { return s.path }
func (s *localStack) Tags() map[apitype.StackTagName]string                  { return s.tags }

func (s *localStack) MergeTags(tags map[apitype.StackTagName]string) {
	if len(tags) == 0 {
		return
	}

	if s.tags == nil {
		s.tags = make(map[apitype.StackTagName]string)
	}

	// Add each new tag to the existing tags, overwriting existing tags with the
	// latest values.
	for k, v := range tags {
		s.tags[k] = v
	}
}

func (s *localStack) Remove(ctx context.Context, force bool) (bool, error) {
	return backend.RemoveStack(ctx, s, force)
}

func (s *localStack) Preview(ctx context.Context, op backend.UpdateOperation) (engine.ResourceChanges, error) {
	return backend.PreviewStack(ctx, s, op)
}

func (s *localStack) Update(ctx context.Context, op backend.UpdateOperation) (engine.ResourceChanges, error) {
	return backend.UpdateStack(ctx, s, op)
}

func (s *localStack) Refresh(ctx context.Context, op backend.UpdateOperation) (engine.ResourceChanges, error) {
	return backend.RefreshStack(ctx, s, op)
}

func (s *localStack) Destroy(ctx context.Context, op backend.UpdateOperation) (engine.ResourceChanges, error) {
	return backend.DestroyStack(ctx, s, op)
}

func (s *localStack) GetLogs(ctx context.Context, query operations.LogQuery) ([]operations.LogEntry, error) {
	return backend.GetStackLogs(ctx, s, query)
}

func (s *localStack) ExportDeployment(ctx context.Context) (*apitype.UntypedDeployment, error) {
	return backend.ExportStackDeployment(ctx, s)
}

func (s *localStack) ImportDeployment(ctx context.Context, deployment *apitype.UntypedDeployment) error {
	return backend.ImportStackDeployment(ctx, s, deployment)
}

type localStackSummary struct {
	s *localStack
}

func newLocalStackSummary(s *localStack) localStackSummary {
	return localStackSummary{s}
}

func (lss localStackSummary) Name() backend.StackReference {
	return lss.s.Ref()
}

func (lss localStackSummary) LastUpdate() *time.Time {
	snap := lss.s.snapshot
	if snap != nil {
		if t := snap.Manifest.Time; !t.IsZero() {
			return &t
		}
	}
	return nil
}

func (lss localStackSummary) ResourceCount() *int {
	snap := lss.s.snapshot
	if snap != nil {
		count := len(snap.Resources)
		return &count
	}
	return nil
}
