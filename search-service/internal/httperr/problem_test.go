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

package httperr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.platform-mesh.io/golang-commons/context/keys"
)

func TestWriteRendersProblemDetails(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=secret&filter.type=premium", nil)
	r = r.WithContext(context.WithValue(r.Context(), keys.RequestIdCtxKey, "req-42"))

	Write(w, r, InvalidRequest.WithDetail("limit must be an integer"))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != ContentType {
		t.Fatalf("expected Content-Type %q, got %q", ContentType, ct)
	}
	if opts := w.Header().Get("X-Content-Type-Options"); opts != "nosniff" {
		t.Fatalf("expected nosniff, got %q", opts)
	}

	var got Problem
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v (body=%s)", err, w.Body.String())
	}

	want := Problem{
		Type:      "https://platform-mesh.io/problems/search/invalid-request",
		Title:     "Invalid request",
		Status:    http.StatusBadRequest,
		Detail:    "limit must be an integer",
		Instance:  "/rest/v1/search",
		Category:  CategoryInvalidRequest,
		RequestID: "req-42",
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestWriteOmitsQueryStringFromInstance(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/rest/v1/search?q=topsecretquery", nil)

	Write(w, r, Internal)

	if body := w.Body.String(); strings.Contains(body, "topsecretquery") {
		t.Fatalf("body leaked the query string: %s", body)
	}
}

func TestWriteOmitsRequestIDWhenAbsent(t *testing.T) {
	w := httptest.NewRecorder()
	Write(w, httptest.NewRequest(http.MethodGet, "/rest/v1/search", nil), Internal)

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, ok := raw["requestId"]; ok {
		t.Fatalf("expected requestId to be omitted, got %v", raw["requestId"])
	}
}

func TestWithDetailDoesNotMutateTemplate(t *testing.T) {
	got := InvalidRequest.WithDetail("limit must be an integer")

	if InvalidRequest.Detail != "" {
		t.Fatalf("template was mutated: %q", InvalidRequest.Detail)
	}
	if got.Detail != "limit must be an integer" {
		t.Fatalf("expected the detail to be set, got %q", got.Detail)
	}
}

func TestProblemTemplatesAreWellFormed(t *testing.T) {
	categories := map[Category]bool{
		CategoryInvalidRequest:  true,
		CategoryAuthentication:  true,
		CategoryAuthorization:   true,
		CategorySearchBackend:   true,
		CategoryUpstreamTimeout: true,
		CategoryNotFound:        true,
		CategoryInternal:        true,
	}

	templates := map[string]Problem{
		"InvalidRequest":            InvalidRequest,
		"InvalidCursor":             InvalidCursor,
		"AuthenticationRequired":    AuthenticationRequired,
		"AuthenticationUnavailable": AuthenticationUnavailable,
		"AccessDenied":              AccessDenied,
		"AuthorizationUnavailable":  AuthorizationUnavailable,
		"SearchBackendUnavailable":  SearchBackendUnavailable,
		"IndexUnavailable":          IndexUnavailable,
		"UpstreamTimeout":           UpstreamTimeout,
		"NotFound":                  NotFound,
		"MethodNotAllowed":          MethodNotAllowed,
		"Internal":                  Internal,
	}

	seen := make(map[string]string, len(templates))
	for name, p := range templates {
		if !strings.HasPrefix(p.Type, typeBaseURI) {
			t.Errorf("%s: type %q is not under %q", name, p.Type, typeBaseURI)
		}
		if p.Title == "" {
			t.Errorf("%s: missing title", name)
		}
		if p.Status < 400 || p.Status > 599 {
			t.Errorf("%s: status %d is not an error status", name, p.Status)
		}
		if !categories[p.Category] {
			t.Errorf("%s: unknown category %q", name, p.Category)
		}
		if other, dup := seen[p.Type]; dup {
			t.Errorf("%s: type %q is already used by %s", name, p.Type, other)
		}
		seen[p.Type] = name
	}
}

func TestBackendTemplatesCarryDetail(t *testing.T) {
	for name, p := range map[string]Problem{
		"AuthenticationRequired":    AuthenticationRequired,
		"AuthenticationUnavailable": AuthenticationUnavailable,
		"AccessDenied":              AccessDenied,
		"AuthorizationUnavailable":  AuthorizationUnavailable,
		"SearchBackendUnavailable":  SearchBackendUnavailable,
		"IndexUnavailable":          IndexUnavailable,
		"UpstreamTimeout":           UpstreamTimeout,
		"NotFound":                  NotFound,
		"MethodNotAllowed":          MethodNotAllowed,
		"Internal":                  Internal,
	} {
		if p.Detail == "" {
			t.Errorf("%s: expected a default detail", name)
		}
	}
}

func TestProblemDetailsDoNotNameInternals(t *testing.T) {
	internals := []string{"opensearch", "openfga", "kcp", "searchindex", "workspace"}

	for name, p := range map[string]Problem{
		"AuthenticationUnavailable": AuthenticationUnavailable,
		"AuthorizationUnavailable":  AuthorizationUnavailable,
		"SearchBackendUnavailable":  SearchBackendUnavailable,
		"IndexUnavailable":          IndexUnavailable,
		"UpstreamTimeout":           UpstreamTimeout,
		"Internal":                  Internal,
	} {
		text := strings.ToLower(p.Title + " " + p.Detail)
		for _, internal := range internals {
			if strings.Contains(text, internal) {
				t.Errorf("%s: names internal component %q: %q", name, internal, text)
			}
		}
	}
}
