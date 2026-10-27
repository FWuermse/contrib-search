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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.platform-mesh.io/search-service/internal/service/search"
	"go.platform-mesh.io/search-service/internal/stringset"

	"k8s.io/apimachinery/pkg/util/sets"
)

// maxShardFailureReasons caps the failure messages from partial responses
const maxShardFailureReasons = 5

type Config struct {
	URL      string
	Username string
	Password string
	Insecure bool
	Timeout  time.Duration
}

type Client struct {
	baseURL  *url.URL
	http     *http.Client
	username string
	password string
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("OpenSearch URL is required")
	}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenSearch URL: %w", err)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if parsed.Scheme == "https" {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.Insecure}
	}

	return &Client{
		baseURL: parsed,
		http: &http.Client{
			Timeout:   cfg.Timeout,
			Transport: transport,
		},
		username: cfg.Username,
		password: cfg.Password,
	}, nil
}

func BuildQueryBody(req search.OpenSearchQuery) ([]byte, error) {
	query := strings.TrimSpace(req.Query)
	mode, err := searchMode(req.Mode)
	if err != nil {
		return nil, err
	}
	fields := lexicalSearchFields(req.Fields)
	semanticFields := prefixedFields("semantic_fields", stringset.DedupeSorted(req.SemanticFields))
	filters := normalizeFilters(req.Filters)
	accountFGAObjects := stringset.DedupeSorted(req.AccountFGAObjects)

	var queryClause map[string]any
	if query == "" {
		queryClause = map[string]any{
			"match_all": map[string]any{},
		}
	} else if mode == search.SearchModeSemantic {
		queryClause, err = buildSemanticQueryClause(query, semanticFields)
		if err != nil {
			return nil, fmt.Errorf("build semantic query clause: %w", err)
		}
	} else {
		simple := map[string]any{
			"query":            query,
			"default_operator": "and",
			"lenient":          true,
		}
		if len(fields) > 0 {
			simple["fields"] = fields
		}
		queryClause = map[string]any{
			"simple_query_string": simple,
		}
	}

	if len(filters) > 0 || len(accountFGAObjects) > 0 {
		filterClauses := make([]map[string]any, 0, len(filters)+1)
		keys := slices.Sorted(maps.Keys(filters))

		for _, field := range keys {
			values := filters[field]
			if len(values) == 0 {
				continue
			}
			filterClauses = append(filterClauses, map[string]any{
				"terms": map[string]any{
					prefixedField("filterable_fields", field): values,
				},
			})
		}
		if len(accountFGAObjects) > 0 {
			filterClauses = append(filterClauses, map[string]any{
				"terms": map[string]any{
					"filterable_fields.account_fga_object": accountFGAObjects,
				},
			})
		}

		queryClause = map[string]any{
			"bool": map[string]any{
				"must":   []any{queryClause},
				"filter": filterClauses,
			},
		}
	}

	body := map[string]any{
		"size":             req.Size,
		"query":            queryClause,
		"track_total_hits": true,
		"sort": []map[string]string{
			{"_score": "desc"},
			{"_id": "asc"},
			{"_index": "asc"},
		},
	}

	if len(req.SearchAfter) > 0 {
		body["search_after"] = req.SearchAfter
	}

	if field := strings.TrimSpace(req.AggregationField); field != "" {
		aggSize := req.Size
		if aggSize <= 0 {
			aggSize = 10
		}
		body["aggs"] = map[string]any{
			"values": map[string]any{
				"terms": map[string]any{
					"field": prefixedField("filterable_fields", field),
					"size":  aggSize,
				},
			},
		}
		if req.Size <= 0 {
			body["size"] = 0
			delete(body, "sort")
		}
	}
	return json.Marshal(body)
}

func buildSemanticQueryClause(query string, semanticFields []string) (map[string]any, error) {
	if len(semanticFields) == 0 {
		return nil, fmt.Errorf("semantic mode requires at least one semantic field")
	}
	if len(semanticFields) == 1 {
		return map[string]any{
			"neural": map[string]any{
				semanticFields[0]: map[string]any{
					"query_text": query,
				},
			},
		}, nil
	}

	shouldClauses := make([]any, 0, len(semanticFields))
	for _, field := range semanticFields {
		shouldClauses = append(shouldClauses, map[string]any{
			"neural": map[string]any{
				field: map[string]any{
					"query_text": query,
				},
			},
		})
	}

	return map[string]any{
		"bool": map[string]any{
			"should":               shouldClauses,
			"minimum_should_match": 1,
		},
	}, nil
}

func searchMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	switch mode {
	case "", search.SearchModeLexical:
		return search.SearchModeLexical, nil
	case search.SearchModeSemantic:
		return search.SearchModeSemantic, nil
	default:
		return "", fmt.Errorf("unsupported search mode %q", raw)
	}
}

func (c *Client) Search(ctx context.Context, query search.OpenSearchQuery) (search.OpenSearchPage, error) {
	indices := stringset.DedupeSorted(query.Indices)
	if len(indices) == 0 {
		return search.OpenSearchPage{}, fmt.Errorf("at least one OpenSearch index is required")
	}

	body, err := BuildQueryBody(search.OpenSearchQuery{
		Query:             query.Query,
		Fields:            query.Fields,
		SemanticFields:    query.SemanticFields,
		Mode:              query.Mode,
		Filters:           query.Filters,
		AccountFGAObjects: query.AccountFGAObjects,
		Size:              query.Size,
		SearchAfter:       query.SearchAfter,
		AggregationField:  query.AggregationField,
	})
	if err != nil {
		return search.OpenSearchPage{}, fmt.Errorf("build OpenSearch query body: %w", err)
	}

	requestURL := c.baseURL.ResolveReference(&url.URL{Path: fmt.Sprintf("/%s/_search", strings.Join(indices, ","))})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return search.OpenSearchPage{}, fmt.Errorf("create OpenSearch request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.username != "" {
		httpReq.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return search.OpenSearchPage{}, fmt.Errorf("execute OpenSearch request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		// A 4xx means OpenSearch rejected the query we built
		sentinel := search.ErrSearchBackend
		if resp.StatusCode < http.StatusInternalServerError {
			sentinel = search.ErrSearchBackendRejected
		}
		return search.OpenSearchPage{}, fmt.Errorf("%w: status %d: %s", sentinel, resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload struct {
		TimedOut bool `json:"timed_out"`
		Shards   struct {
			Total      int `json:"total"`
			Successful int `json:"successful"`
			Skipped    int `json:"skipped"`
			Failed     int `json:"failed"`
			Failures   []struct {
				Index  string `json:"index"`
				Reason struct {
					Type   string `json:"type"`
					Reason string `json:"reason"`
				} `json:"reason"`
			} `json:"failures"`
		} `json:"_shards"`
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Index  string         `json:"_index"`
				ID     string         `json:"_id"`
				Score  float64        `json:"_score"`
				Sort   []any          `json:"sort"`
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations map[string]struct {
			Buckets []struct {
				Key any `json:"key"`
			} `json:"buckets"`
		} `json:"aggregations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return search.OpenSearchPage{}, fmt.Errorf("decode OpenSearch response: %w", err)
	}

	hits := make([]search.OpenSearchHit, 0, len(payload.Hits.Hits))
	for _, hit := range payload.Hits.Hits {
		hits = append(hits, search.OpenSearchHit{
			Index:  hit.Index,
			ID:     hit.ID,
			Score:  hit.Score,
			Sort:   hit.Sort,
			Source: hit.Source,
		})
	}

	var values []string
	if agg, ok := payload.Aggregations["values"]; ok {
		values = make([]string, 0, len(agg.Buckets))
		for _, b := range agg.Buckets {
			if value := scalarString(b.Key); value != "" {
				values = append(values, value)
			}
		}
		slices.Sort(values)
	}

	// Process OpenSearch shard errors: if some shards fail, hits and TotalCount
	// can be partial. If all shards fail (e.g. mapping errors) we need to dedupe the messages
	var shardFailures []string
	seenFailures := sets.New[string]()
	for _, failure := range payload.Shards.Failures {
		reason := strings.TrimSpace(fmt.Sprintf("%s: %s: %s", failure.Index, failure.Reason.Type, failure.Reason.Reason))
		if seenFailures.Has(reason) {
			continue
		}

		seenFailures.Insert(reason)
		if len(shardFailures) == maxShardFailureReasons {
			break
		}

		shardFailures = append(shardFailures, reason)
	}

	return search.OpenSearchPage{
		Hits:              hits,
		AggregationValues: values,
		TotalCount:        payload.Hits.Total.Value,
		TimedOut:          payload.TimedOut,
		ShardsTotal:       payload.Shards.Total,
		ShardsFailed:      payload.Shards.Failed,
		ShardFailures:     shardFailures,
	}, nil
}

func normalizeFilters(filters map[string][]string) map[string][]string {
	if len(filters) == 0 {
		return nil
	}

	out := make(map[string][]string, len(filters))
	for field, rawValues := range filters {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}

		values := stringset.DedupeSorted(rawValues)
		if len(values) == 0 {
			continue
		}
		out[field] = values
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func prefixedFields(prefix string, fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if prefixed := prefixedField(prefix, field); prefixed != "" {
			out = append(out, prefixed)
		}
	}
	return stringset.DedupeSorted(out)
}

func lexicalSearchFields(fields []string) []string {
	prefixed := prefixedFields("default_fields", stringset.DedupeSorted(fields))
	return stringset.DedupeSorted(append(prefixed, defaultLexicalSearchFields...))
}

var defaultLexicalSearchFields = []string{
	"account_name",
	"api_group",
	"api_version",
	"cluster_name",
	"kind",
	"name",
	"namespace",
	"organization_name",
	"payload_text",
	"workspace_path",
}

func prefixedField(prefix, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	for _, existingPrefix := range []string{"default_fields.", "semantic_fields.", "filterable_fields."} {
		if strings.HasPrefix(field, existingPrefix) {
			return field
		}
	}
	return prefix + "." + field
}

func scalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", typed)
	default:
		return ""
	}
}
