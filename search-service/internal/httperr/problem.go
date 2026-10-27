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

// Package httperr renders API errors as RFC 9457 problem details, so callers can
// tell an invalid request from an authorization failure or a search backend
// outage without the service leaking internal names, queries, or stack traces.
package httperr

import (
	"encoding/json"
	"net/http"

	cmw "go.platform-mesh.io/golang-commons/middleware"
)

const ContentType = "application/json"

const typeBaseURI = "https://platform-mesh.io/problems/search/"

// Category is the coarse classification a caller can branch on.
type Category string

const (
	CategoryInvalidRequest  Category = "invalid_request"
	CategoryAuthentication  Category = "authentication"
	CategoryAuthorization   Category = "authorization"
	CategorySearchBackend   Category = "search_backend"
	CategoryUpstreamTimeout Category = "upstream_timeout"
	CategoryNotFound        Category = "not_found"
	CategoryInternal        Category = "internal"
)

// Problem is an RFC 9457 problem details object. Category and RequestID are
// extension members: Category classifies the failure for programmatic handling,
// RequestID correlates the response with the service logs.
type Problem struct {
	Type      string   `json:"type"`
	Title     string   `json:"title"`
	Status    int      `json:"status"`
	Detail    string   `json:"detail,omitempty"`
	Instance  string   `json:"instance,omitempty"`
	Category  Category `json:"category"`
	RequestID string   `json:"requestId,omitempty"`
}

// The complete set of problems the API can return. These are templates:
// WithDetail copies them, so the package-level values are never mutated.
var (
	InvalidRequest = Problem{
		Type:     typeBaseURI + "invalid-request",
		Title:    "Invalid request",
		Status:   http.StatusBadRequest,
		Category: CategoryInvalidRequest,
	}

	InvalidCursor = Problem{
		Type:     typeBaseURI + "invalid-cursor",
		Title:    "Invalid pagination cursor",
		Status:   http.StatusBadRequest,
		Category: CategoryInvalidRequest,
	}

	AuthenticationRequired = Problem{
		Type:     typeBaseURI + "authentication-required",
		Title:    "Authentication required",
		Status:   http.StatusUnauthorized,
		Detail:   "a valid bearer token is required",
		Category: CategoryAuthentication,
	}

	AuthenticationUnavailable = Problem{
		Type:     typeBaseURI + "authentication-unavailable",
		Title:    "Authentication unavailable",
		Status:   http.StatusInternalServerError,
		Detail:   "the token could not be validated",
		Category: CategoryAuthentication,
	}

	AccessDenied = Problem{
		Type:     typeBaseURI + "access-denied",
		Title:    "Access denied",
		Status:   http.StatusForbidden,
		Detail:   "access denied for the requested resource",
		Category: CategoryAuthorization,
	}

	AuthorizationUnavailable = Problem{
		Type:     typeBaseURI + "authorization-unavailable",
		Title:    "Authorization unavailable",
		Status:   http.StatusInternalServerError,
		Detail:   "permissions could not be evaluated",
		Category: CategoryAuthorization,
	}

	SearchBackendUnavailable = Problem{
		Type:     typeBaseURI + "search-backend-unavailable",
		Title:    "Search backend unavailable",
		Status:   http.StatusInternalServerError,
		Detail:   "the search backend could not process the request",
		Category: CategorySearchBackend,
	}

	SearchQueryRejected = Problem{
		Type:     typeBaseURI + "search-query-rejected",
		Title:    "Search query rejected",
		Status:   http.StatusInternalServerError,
		Detail:   "the search backend rejected the generated query",
		Category: CategorySearchBackend,
	}

	IndexUnavailable = Problem{
		Type:     typeBaseURI + "index-unavailable",
		Title:    "Search index unavailable",
		Status:   http.StatusInternalServerError,
		Detail:   "no search index is available for this organization",
		Category: CategorySearchBackend,
	}

	UpstreamTimeout = Problem{
		Type:     typeBaseURI + "upstream-timeout",
		Title:    "Upstream timeout",
		Status:   http.StatusInternalServerError,
		Detail:   "the request to an upstream system timed out",
		Category: CategoryUpstreamTimeout,
	}

	NotFound = Problem{
		Type:     typeBaseURI + "not-found",
		Title:    "Not found",
		Status:   http.StatusNotFound,
		Detail:   "the requested endpoint does not exist",
		Category: CategoryNotFound,
	}

	MethodNotAllowed = Problem{
		Type:     typeBaseURI + "method-not-allowed",
		Title:    "Method not allowed",
		Status:   http.StatusMethodNotAllowed,
		Detail:   "the HTTP method is not allowed for this endpoint",
		Category: CategoryInvalidRequest,
	}

	Internal = Problem{
		Type:     typeBaseURI + "internal-error",
		Title:    "Internal server error",
		Status:   http.StatusInternalServerError,
		Detail:   "the request could not be completed",
		Category: CategoryInternal,
	}
)

// WithDetail returns a copy of p carrying detail, leaving the template untouched.
func (p Problem) WithDetail(detail string) Problem {
	p.Detail = detail
	return p
}

// Write renders p as an RFC 9457 problem details response.
func Write(w http.ResponseWriter, r *http.Request, p Problem) {
	p.Instance = r.URL.Path
	p.RequestID = cmw.GetRequestId(r.Context())

	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(p.Status)

	_ = json.NewEncoder(w).Encode(p)
}
