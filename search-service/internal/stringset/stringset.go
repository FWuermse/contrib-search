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

// Package stringset holds string helpers shared by the client and service layers.
package stringset

import (
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
)

// DedupeSorted trims every value, drops the empty ones, removes duplicates and
// sorts what remains. The result is never nil.
func DedupeSorted(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			trimmed = append(trimmed, value)
		}
	}

	return sets.List(sets.New(trimmed...))
}
