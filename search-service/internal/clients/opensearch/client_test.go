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

package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.platform-mesh.io/search-service/internal/service/search"
)

func TestClientSearchReturnsExactTotalCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got := body["track_total_hits"]; got != true {
			t.Fatalf("track_total_hits = %v, want true", got)
		}
		_, _ = w.Write([]byte(`{"hits":{"total":{"value":42,"relation":"eq"},"hits":[]}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{URL: server.URL})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	page, err := client.Search(context.Background(), search.OpenSearchQuery{
		Indices: []string{"idx"},
		Query:   "hello",
		Size:    10,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if page.TotalCount != 42 {
		t.Fatalf("TotalCount = %d, want 42", page.TotalCount)
	}
}

func TestBuildQueryBodyWithoutSearchAfter(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:  "hello",
		Fields: []string{"name", "description"},
		Size:   20,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if payload["size"].(float64) != 20 {
		t.Fatalf("unexpected size: %v", payload["size"])
	}
	if _, ok := payload["search_after"]; ok {
		t.Fatalf("search_after should not be set")
	}

	sort := payload["sort"].([]any)
	if len(sort) != 3 {
		t.Fatalf("expected 3 sort fields")
	}

	query := payload["query"].(map[string]any)
	simple := query["simple_query_string"].(map[string]any)
	// Indices disagree on the type of a dynamically mapped default_fields.* path:
	// without lenient, a text query against the index that mapped it as long
	// fails the whole request with number_format_exception.
	if simple["lenient"] != true {
		t.Fatalf("lenient = %v, want true", simple["lenient"])
	}
	fields := simple["fields"].([]any)
	if fields[0] != "account_name" || fields[1] != "api_group" {
		t.Fatalf("expected default lexical fields first, got %v", fields)
	}
	if !containsField(fields, "default_fields.description") || !containsField(fields, "default_fields.name") {
		t.Fatalf("unexpected search fields: %v", fields)
	}
	if !containsField(fields, "payload_text") {
		t.Fatalf("expected payload_text search field, got %v", fields)
	}
}

func TestBuildQueryBodyWithSearchAfter(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:       "hello",
		Fields:      []string{"name"},
		Size:        10,
		SearchAfter: []any{1.0, "id-1", "idx"},
		Filters: map[string][]string{
			"status": {"Ready"},
		},
		AccountFGAObjects: []string{"core_platform-mesh_io_account:cluster/acme"},
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	searchAfter := payload["search_after"].([]any)
	if len(searchAfter) != 3 {
		t.Fatalf("expected 3 search_after values")
	}

	query := payload["query"].(map[string]any)
	boolQuery := query["bool"].(map[string]any)
	if _, ok := boolQuery["filter"]; !ok {
		t.Fatalf("expected filter clause")
	}
	filter := boolQuery["filter"].([]any)
	terms := filter[0].(map[string]any)["terms"].(map[string]any)
	if _, ok := terms["filterable_fields.status"]; !ok {
		t.Fatalf("expected filterable_fields.status filter, got %v", terms)
	}
	accountTerms := filter[1].(map[string]any)["terms"].(map[string]any)
	if got := accountTerms["filterable_fields.account_fga_object"]; len(got.([]any)) != 1 || got.([]any)[0] != "core_platform-mesh_io_account:cluster/acme" {
		t.Fatalf("unexpected account authorization filter: %v", got)
	}
	if got := payload["track_total_hits"]; got != true {
		t.Fatalf("track_total_hits = %v, want true", got)
	}
}

func TestBuildQueryBodyWithoutQueryUsesMatchAll(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query: "",
		Size:  5,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	query := payload["query"].(map[string]any)
	if _, ok := query["match_all"]; !ok {
		t.Fatalf("expected match_all query")
	}
}

func TestBuildQueryBodySemanticSingleField(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:          "hello",
		Mode:           search.SearchModeSemantic,
		SemanticFields: []string{"description"},
		Size:           20,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	query := payload["query"].(map[string]any)
	neural := query["neural"].(map[string]any)
	description := neural["semantic_fields.description"].(map[string]any)
	if got := description["query_text"]; got != "hello" {
		t.Fatalf("unexpected query_text: %v", got)
	}
}

func TestBuildQueryBodySemanticMultipleFieldsWithFilters(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:          "hello",
		Mode:           search.SearchModeSemantic,
		SemanticFields: []string{"description", "spec.summary"},
		Filters: map[string][]string{
			"status": {"Ready"},
		},
		Size: 10,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	query := payload["query"].(map[string]any)
	boolQuery := query["bool"].(map[string]any)
	must := boolQuery["must"].([]any)
	innerBool := must[0].(map[string]any)["bool"].(map[string]any)
	should := innerBool["should"].([]any)
	if len(should) != 2 {
		t.Fatalf("expected 2 semantic should clauses, got %d", len(should))
	}
	if _, ok := boolQuery["filter"]; !ok {
		t.Fatalf("expected filter clause")
	}
}

func TestBuildQueryBodyAggregationUsesFilterableFieldPrefix(t *testing.T) {
	body, err := BuildQueryBody(search.OpenSearchQuery{
		AggregationField: "spec.replicas",
		Size:             0,
	})
	if err != nil {
		t.Fatalf("BuildQueryBody returned error: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	aggs := payload["aggs"].(map[string]any)
	values := aggs["values"].(map[string]any)
	terms := values["terms"].(map[string]any)
	if got := terms["field"]; got != "filterable_fields.spec.replicas" {
		t.Fatalf("aggregation field = %v, want filterable_fields.spec.replicas", got)
	}
	if got := payload["size"]; got != float64(0) {
		t.Fatalf("size = %v, want 0", got)
	}
}

// A mapping conflict fails only the shards that hold the conflicting index, and
// OpenSearch still answers 200. The page has to carry that, or a partial result
// is indistinguishable from a complete one with a smaller total.
func TestClientSearchSurfacesShardFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"timed_out": false,
			"_shards": {"total": 3, "successful": 2, "skipped": 0, "failed": 1, "failures": [
				{"index": "idx-b", "reason": {"type": "number_format_exception", "reason": "For input string: \"test\""}},
				{"index": "idx-b", "reason": {"type": "number_format_exception", "reason": "For input string: \"test\""}}
			]},
			"hits": {"total": {"value": 7, "relation": "eq"}, "hits": []}
		}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{URL: server.URL})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	page, err := client.Search(context.Background(), search.OpenSearchQuery{
		Indices: []string{"idx-a", "idx-b"},
		Query:   "test",
		Size:    10,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if !page.Partial() {
		t.Fatalf("Partial() = false, want true")
	}
	if page.ShardsTotal != 3 || page.ShardsFailed != 1 {
		t.Fatalf("shards total/failed = %d/%d, want 3/1", page.ShardsTotal, page.ShardsFailed)
	}
	if len(page.ShardFailures) != 1 {
		t.Fatalf("ShardFailures = %v, want the duplicate reason collapsed to one", page.ShardFailures)
	}
	if want := `idx-b: number_format_exception: For input string: "test"`; page.ShardFailures[0] != want {
		t.Fatalf("ShardFailures[0] = %q, want %q", page.ShardFailures[0], want)
	}
}

func TestClientSearchCompleteResponseIsNotPartial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"timed_out":false,"_shards":{"total":2,"successful":2,"failed":0},"hits":{"total":{"value":1},"hits":[]}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{URL: server.URL})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	page, err := client.Search(context.Background(), search.OpenSearchQuery{Indices: []string{"idx"}, Size: 10})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if page.Partial() {
		t.Fatalf("Partial() = true for a fully successful response")
	}
	if page.ShardFailures != nil {
		t.Fatalf("ShardFailures = %v, want nil", page.ShardFailures)
	}
}

func TestClientSearchTimedOutIsPartial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"timed_out":true,"_shards":{"total":2,"successful":2,"failed":0},"hits":{"total":{"value":1},"hits":[]}}`))
	}))
	defer server.Close()

	client, err := NewClient(Config{URL: server.URL})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	page, err := client.Search(context.Background(), search.OpenSearchQuery{Indices: []string{"idx"}, Size: 10})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !page.Partial() {
		t.Fatalf("Partial() = false for a timed-out response")
	}
}

// A 4xx is a query we built wrong; a 5xx is a backend that is down. They map to
// different sentinels so the router can tell the caller which one happened.
func TestClientSearchErrorStatusSentinels(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		wantSentinel error
		otherErr     error
	}{
		{"rejected query", http.StatusBadRequest, search.ErrSearchBackendRejected, search.ErrSearchBackend},
		{"backend down", http.StatusServiceUnavailable, search.ErrSearchBackend, search.ErrSearchBackendRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"error":{"type":"number_format_exception"}}`))
			}))
			defer server.Close()

			client, err := NewClient(Config{URL: server.URL})
			if err != nil {
				t.Fatalf("NewClient returned error: %v", err)
			}
			_, err = client.Search(context.Background(), search.OpenSearchQuery{Indices: []string{"idx"}, Query: "test", Size: 10})
			if err == nil {
				t.Fatalf("Search returned no error for status %d", tt.status)
			}
			if !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("error %v does not match %v", err, tt.wantSentinel)
			}
			if errors.Is(err, tt.otherErr) {
				t.Fatalf("error %v unexpectedly matches %v", err, tt.otherErr)
			}
			if !strings.Contains(err.Error(), "number_format_exception") {
				t.Fatalf("error %v drops the upstream body", err)
			}
		})
	}
}

func containsField(fields []any, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}
