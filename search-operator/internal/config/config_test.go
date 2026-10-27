/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"testing"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestSearchableResourcesFlag(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    []schema.GroupVersionKind
		wantErr bool
	}{
		{
			name: "single value",
			args: []string{"--searchable-resources=apps/v1/Deployment"},
			want: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
			},
		},
		{
			name: "comma-separated values",
			args: []string{"--searchable-resources=apps/v1/Deployment,core/v1/ConfigMap"},
			want: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "core", Version: "v1", Kind: "ConfigMap"},
			},
		},
		{
			name: "repeated flag",
			args: []string{"--searchable-resources=apps/v1/Deployment", "--searchable-resources=core/v1/ConfigMap"},
			want: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "core", Version: "v1", Kind: "ConfigMap"},
			},
		},
		{
			name:    "invalid format",
			args:    []string{"--searchable-resources=apps/v1"},
			wantErr: true,
		},
		{
			name: "empty tokens are skipped",
			args: []string{"--searchable-resources=apps/v1/Deployment,,core/v1/ConfigMap"},
			want: []schema.GroupVersionKind{
				{Group: "apps", Version: "v1", Kind: "Deployment"},
				{Group: "core", Version: "v1", Kind: "ConfigMap"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewOperatorConfig()
			fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
			cfg.AddFlags(fs)

			err := fs.Parse(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.SearchableResources) != len(tc.want) {
				t.Fatalf("got %d GVKs, want %d: %v", len(cfg.SearchableResources), len(tc.want), cfg.SearchableResources)
			}
			for i, gvk := range cfg.SearchableResources {
				if gvk != tc.want[i] {
					t.Errorf("GVK[%d]: got %v, want %v", i, gvk, tc.want[i])
				}
			}
		})
	}
}
