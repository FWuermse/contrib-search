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

package kcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"go.platform-mesh.io/golang-commons/logger"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type OrgAccessValidator struct {
	clientset kubernetes.Interface
	log       *logger.Logger
}

func NewOrgAccessValidator(restCfg *rest.Config, tokenReviewWorkspacePath string, log *logger.Logger) (*OrgAccessValidator, error) {
	cfg := rest.CopyConfig(restCfg)
	cfg.Host = replaceClusterPath(cfg.Host, tokenReviewWorkspacePath)

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kcp kubernetes client: %w", err)
	}
	return &OrgAccessValidator{clientset: cs, log: log}, nil
}

// replaceClusterPath replaces the /clusters/<path> portion of a KCP host URL
// with the given workspace path so TokenReview targets the correct workspace.
func replaceClusterPath(host, workspacePath string) string {
	u, err := url.Parse(host)
	if err != nil {
		return host
	}
	if idx := strings.Index(u.Path, "/clusters/"); idx >= 0 {
		u.Path = u.Path[:idx] + "/clusters/" + workspacePath
		return u.String()
	}
	return host
}

// ValidateTokenForOrg returns true when kcp authenticated the JWT.
// A TokenReview authenticated=false means the JWT is invalid.
// OpenFGA remains responsible for authorizing individual search results.
func (v *OrgAccessValidator) ValidateTokenForOrg(ctx context.Context, authHeader, org string) (bool, error) {
	token, err := bearerToken(authHeader)
	if err != nil {
		return false, err
	}

	tr, err := v.clientset.AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil {
		v.log.Error().
			Err(err).
			Str("organization", org).
			Msg("TokenReview API call failed")
		return false, fmt.Errorf("execute TokenReview: %w", err)
	}

	if !tr.Status.Authenticated {
		v.log.Warn().
			Str("organization", org).
			Str("tokenReviewError", tr.Status.Error).
			Msg("kcp rejected the JWT")
	}
	return tr.Status.Authenticated, nil
}

func bearerToken(authHeader string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(authHeader))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", fmt.Errorf("invalid authorization header")
	}
	return parts[1], nil
}
