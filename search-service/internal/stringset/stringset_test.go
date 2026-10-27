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

package stringset

import (
	"slices"
	"testing"
)

func TestDedupeSorted(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil input", input: nil, want: []string{}},
		{name: "empty input", input: []string{}, want: []string{}},
		{name: "only blanks", input: []string{"", "   ", "\t"}, want: []string{}},
		{name: "sorts", input: []string{"name", "cluster_name", "kind"}, want: []string{"cluster_name", "kind", "name"}},
		{name: "trims", input: []string{"  name  ", "\tkind\n"}, want: []string{"kind", "name"}},
		{name: "drops duplicates", input: []string{"name", "name", "kind"}, want: []string{"kind", "name"}},
		{
			name:  "dedupes after trimming",
			input: []string{"name", " name", "name "},
			want:  []string{"name"},
		},
		{
			name:  "keeps case-distinct values",
			input: []string{"Name", "name"},
			want:  []string{"Name", "name"},
		},
		{
			name:  "drops blanks but keeps the rest",
			input: []string{"spec.replicas", "", "  ", "metadata.name"},
			want:  []string{"metadata.name", "spec.replicas"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DedupeSorted(tc.input)

			// SearchResource marshals the result with omitzero, where nil drops the
			// JSON member and an empty slice renders as []. The API sends [].
			if got == nil {
				t.Fatalf("DedupeSorted(%q) = nil, want a non-nil slice", tc.input)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("DedupeSorted(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// The helper is called on CRD spec fields that the caller keeps using afterwards.
func TestDedupeSortedDoesNotMutateInput(t *testing.T) {
	input := []string{"name", "  kind ", "name"}
	want := []string{"name", "  kind ", "name"}

	DedupeSorted(input)

	if !slices.Equal(input, want) {
		t.Fatalf("input mutated to %q, want %q", input, want)
	}
}
