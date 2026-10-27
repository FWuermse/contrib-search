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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ErrInvalidRequest        = errors.New("invalid request")
	ErrInvalidCursor         = errors.New("invalid cursor")
	ErrUnauthorized          = errors.New("unauthorized")
	ErrForbidden             = errors.New("forbidden")
	ErrSearchBackend         = errors.New("search backend failure")
	ErrAuthzBackend          = errors.New("authorization backend failure")
	ErrIndexUnavailable      = errors.New("no search index available")
	ErrUpstreamTimeout       = errors.New("upstream timeout")
	ErrSearchBackendRejected = errors.New("search backend rejected the request")
	ErrFGARelationNotFound   = errors.New("OpenFGA relation not found")
)

// backendErr wraps an upstream failure, except for timeouts and rejected queries:
// those get ErrUpstreamTimeout or ErrSearchBackendRejected
func backendErr(sentinel error, op string, err error) error {
	if isTimeout(err) {
		return fmt.Errorf("%w: %s: %v", ErrUpstreamTimeout, op, err)
	}
	if errors.Is(err, ErrSearchBackendRejected) {
		sentinel = ErrSearchBackendRejected
	}
	return fmt.Errorf("%w: %s: %v", sentinel, op, err)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
