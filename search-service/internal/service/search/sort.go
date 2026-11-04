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

package search

import (
	"fmt"
	"strings"

	"go.platform-mesh.io/search-service/internal/stringset"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	SortOrderAsc  = "asc"
	SortOrderDesc = "desc"
)

// normalizeSort validates the requested sort. An empty field keeps the default
// relevance ordering; an order without a field is rejected rather than ignored.
func normalizeSort(rawField, rawOrder string) (string, string, error) {
	field := strings.TrimSpace(rawField)
	order := strings.ToLower(strings.TrimSpace(rawOrder))
	if field == "" {
		if order != "" {
			return "", "", fmt.Errorf("%w: order requires a sort field", ErrInvalidRequest)
		}
		return "", "", nil
	}

	switch order {
	case "", SortOrderAsc:
		return field, SortOrderAsc, nil
	case SortOrderDesc:
		return field, SortOrderDesc, nil
	default:
		return "", "", fmt.Errorf("%w: unsupported sort order %q", ErrInvalidRequest, rawOrder)
	}
}

// cursorSort is the sort a cursor was issued for; empty means relevance.
func cursorSort(field, order string) string {
	if field == "" {
		return ""
	}
	return field + ":" + order
}

// sortSubfield is the subfield the search-operator gives every typed default
// field: a lowercased keyword for strings, the field's own type otherwise.
const sortSubfield = "sort"

// resolveSortField resolves a requested sort field to its OpenSearch path. Only
// fields the index is searched in qualify: filterable fields are keywords and
// sort directly, default fields sort through their subfield when the schema
// gave them a scalar type. Objects, maps and arrays of objects cannot be sorted.
func resolveSortField(field, resource string, ref SearchIndexRef) (string, error) {
	if field == "" {
		return "", nil
	}
	if sets.New(stringset.DedupeSorted(ref.FilterableFields)...).Has(field) {
		return "filterable_fields." + field, nil
	}
	if sets.New(stringset.DedupeSorted(ref.DefaultFields)...).Has(field) && ref.DefaultFieldTypes[field] != "" {
		return "default_fields." + field + "." + sortSubfield, nil
	}

	return "", fmt.Errorf("%w: field %q is not sortable for resource %q", ErrInvalidRequest, field, resource)
}
