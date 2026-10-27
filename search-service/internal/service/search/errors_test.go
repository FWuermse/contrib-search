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
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestBackendErrClassifiesTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		sentinel error
		cause    error
		want     error
	}{
		{
			name: "grpc deadline", sentinel: ErrAuthzBackend,
			cause: fmt.Errorf("list accounts: %w", status.Error(codes.DeadlineExceeded, "deadline")), want: ErrUpstreamTimeout,
		},
		{
			name:     "context deadline",
			sentinel: ErrSearchBackend,
			cause:    context.DeadlineExceeded,
			want:     ErrUpstreamTimeout,
		},
		{
			name:     "wrapped context deadline",
			sentinel: ErrSearchBackend,
			cause:    fmt.Errorf("execute request: %w", context.DeadlineExceeded),
			want:     ErrUpstreamTimeout,
		},
		{
			// What an http.Client Timeout actually surfaces.
			name:     "http client timeout",
			sentinel: ErrSearchBackend,
			cause:    &url.Error{Op: "Post", URL: "http://opensearch:9200/_search", Err: timeoutError{}},
			want:     ErrUpstreamTimeout,
		},
		{
			name:     "timeout from the authorization backend",
			sentinel: ErrAuthzBackend,
			cause:    context.DeadlineExceeded,
			want:     ErrUpstreamTimeout,
		},
		{
			name:     "connection refused stays a backend failure",
			sentinel: ErrSearchBackend,
			cause:    errors.New("dial tcp: connection refused"),
			want:     ErrSearchBackend,
		},
		{
			name:     "cancellation is not a timeout",
			sentinel: ErrSearchBackend,
			cause:    context.Canceled,
			want:     ErrSearchBackend,
		},
		{
			name:     "authorization failure keeps its sentinel",
			sentinel: ErrAuthzBackend,
			cause:    errors.New("no store found"),
			want:     ErrAuthzBackend,
		},
		{
			// backendErr formats the cause with %v, so the promotion has to happen
			// here or the rejection is lost before the router sees it.
			name:     "rejected query is promoted",
			sentinel: ErrSearchBackend,
			cause:    fmt.Errorf("%w: status 400: number_format_exception", ErrSearchBackendRejected),
			want:     ErrSearchBackendRejected,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := backendErr(tc.sentinel, "query backend", tc.cause)

			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if tc.want != tc.sentinel && errors.Is(err, tc.sentinel) {
				t.Fatalf("promoted error should not also match %v: %v", tc.sentinel, err)
			}
			if !strings.Contains(err.Error(), "query backend") {
				t.Fatalf("expected the operation to be wrapped, got %v", err)
			}
		})
	}
}

// The sentinels must stay mutually exclusive: the router picks a problem with a
// first-match switch, so an error matching two sentinels would map by accident.
func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := map[string]error{
		"ErrInvalidRequest":        ErrInvalidRequest,
		"ErrInvalidCursor":         ErrInvalidCursor,
		"ErrUnauthorized":          ErrUnauthorized,
		"ErrForbidden":             ErrForbidden,
		"ErrSearchBackend":         ErrSearchBackend,
		"ErrSearchBackendRejected": ErrSearchBackendRejected,
		"ErrAuthzBackend":          ErrAuthzBackend,
		"ErrIndexUnavailable":      ErrIndexUnavailable,
		"ErrUpstreamTimeout":       ErrUpstreamTimeout,
	}

	for name, err := range sentinels {
		for otherName, other := range sentinels {
			if name == otherName {
				continue
			}
			if errors.Is(err, other) {
				t.Errorf("%s also matches %s", name, otherName)
			}
		}
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}
