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
	"context"
	"errors"
	"testing"
)

func sortTestService(searcher *fakeSearcher) *Service {
	return NewService(
		fakeResolver{index: SearchIndexRef{
			Resource:      "components",
			IndexName:     "idx-components",
			DefaultFields: []string{"spec.displayName", "spec.extra", "spec.stars"},
			DefaultFieldTypes: map[string]string{
				"spec.displayName": "string",
				"spec.stars":       "integer",
			},
			FilterableFields: []string{"kind"},
		}},
		searcher,
		&fakeAuthorizer{results: []AuthorizationResult{{Allowed: []bool{true, true}}}},
		nil,
		ServiceConfig{DefaultLimit: 1, MaxLimit: 100, FetchBatchSize: 2, MaxScannedHits: 1000},
	)
}

func TestSearchSortsByResolvedField(t *testing.T) {
	searcher := &fakeSearcher{
		pages: []OpenSearchPage{{Hits: []OpenSearchHit{
			{ID: "1", Sort: []any{"alpha", 1.0, "1", "idx-components"}, Source: map[string]any{"id": "1"}},
			{ID: "2", Sort: []any{"beta", 1.0, "2", "idx-components"}, Source: map[string]any{"id": "2"}},
		}}},
	}
	svc := sortTestService(searcher)

	resp, err := svc.Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Resource:     "components",
		SortField:    " spec.displayName ",
		SortOrder:    "DESC",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := searcher.reqs[0]; got.SortField != "default_fields.spec.displayName.sort" || got.SortOrder != SortOrderDesc {
		t.Fatalf("unexpected OpenSearch sort: field=%q order=%q", got.SortField, got.SortOrder)
	}

	if resp.NextCursor == nil {
		t.Fatalf("expected next cursor")
	}
	cursor, err := DecodeCursor(*resp.NextCursor)
	if err != nil {
		t.Fatalf("DecodeCursor returned error: %v", err)
	}
	if cursor.Sort != "spec.displayName:desc" {
		t.Fatalf("cursor sort = %q, want spec.displayName:desc", cursor.Sort)
	}

	// The cursor resumes the same sort.
	next := &fakeSearcher{}
	if _, err := sortTestService(next).Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Resource:     "components",
		SortField:    "spec.displayName",
		SortOrder:    "desc",
		Limit:        1,
		Cursor:       *resp.NextCursor,
	}); err != nil {
		t.Fatalf("Search with cursor returned error: %v", err)
	}
	if got := next.reqs[0]; got.SortField != "default_fields.spec.displayName.sort" || len(got.SearchAfter) != 4 || got.SearchAfter[0] != "alpha" {
		t.Fatalf("unexpected resumed query: %+v", got)
	}
}

func TestSearchDefaultsSortOrderToAscending(t *testing.T) {
	searcher := &fakeSearcher{}
	if _, err := sortTestService(searcher).Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Resource:     "components",
		SortField:    "kind",
	}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := searcher.reqs[0]; got.SortField != "filterable_fields.kind" || got.SortOrder != SortOrderAsc {
		t.Fatalf("unexpected OpenSearch sort: field=%q order=%q", got.SortField, got.SortOrder)
	}
}

func TestSearchWithoutSortKeepsRelevanceOrder(t *testing.T) {
	searcher := &fakeSearcher{}
	if _, err := sortTestService(searcher).Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Resource:     "components",
	}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := searcher.reqs[0]; got.SortField != "" || got.SortOrder != "" {
		t.Fatalf("unexpected OpenSearch sort: field=%q order=%q", got.SortField, got.SortOrder)
	}
}

func TestSearchRejectsInvalidSort(t *testing.T) {
	tests := []struct {
		name string
		req  SearchRequest
	}{
		{
			name: "field not searched in",
			req:  SearchRequest{Resource: "components", SortField: "spec.secret"},
		},
		{
			name: "prefixed document path",
			req:  SearchRequest{Resource: "components", SortField: "default_fields.spec.stars"},
		},
		{
			name: "unsupported order",
			req:  SearchRequest{Resource: "components", SortField: "kind", SortOrder: "up"},
		},
		{
			name: "order without field",
			req:  SearchRequest{Resource: "components", SortOrder: "desc"},
		},
		{
			name: "across all resources",
			req:  SearchRequest{SortField: "kind"},
		},
		{
			name: "untyped default field",
			req:  SearchRequest{Resource: "components", SortField: "spec.extra"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fakeSearcher{}
			tt.req.Organization = "acme"
			tt.req.User = "alice@example.com"

			_, err := sortTestService(searcher).Search(context.Background(), tt.req)
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
			if len(searcher.reqs) != 0 {
				t.Fatalf("OpenSearch must not be queried for an invalid sort")
			}
		})
	}
}

func TestSearchSortsTypedDefaultFieldThroughSortSubfield(t *testing.T) {
	searcher := &fakeSearcher{}
	if _, err := sortTestService(searcher).Search(context.Background(), SearchRequest{
		Organization: "acme",
		User:         "alice@example.com",
		Resource:     "components",
		SortField:    "spec.stars",
	}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if got := searcher.reqs[0].SortField; got != "default_fields.spec.stars.sort" {
		t.Fatalf("OpenSearch sort field = %q, want default_fields.spec.stars.sort", got)
	}
}

func TestSearchRejectsCursorFromDifferentSort(t *testing.T) {
	cursor, err := EncodeCursor(CursorState{
		Org:         "acme",
		QueryHash:   queryHash("*"),
		Mode:        SearchModeLexical,
		Resource:    "components",
		Sort:        "kind:asc",
		Limit:       20,
		SearchAfter: []any{"Component", 1.0, "x"},
	})
	if err != nil {
		t.Fatalf("EncodeCursor returned error: %v", err)
	}

	for _, req := range []SearchRequest{
		{SortField: "kind", SortOrder: "desc"},
		{SortField: "spec.stars"},
		{},
	} {
		req.Organization = "acme"
		req.User = "alice@example.com"
		req.Resource = "components"
		req.Limit = 20
		req.Cursor = cursor

		if _, err := sortTestService(&fakeSearcher{}).Search(context.Background(), req); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("sort %q/%q: expected ErrInvalidCursor, got %v", req.SortField, req.SortOrder, err)
		}
	}
}
