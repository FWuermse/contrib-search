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

package config

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

type OperatorConfig struct {
	KCPKubeconfig              string
	APIExportEndpointSliceName string
	SearchableResources        []schema.GroupVersionKind
	OpenSearchURL              string
	OpenSearchUsername         string
	OpenSearchPassword         string
	OpenSearchInsecure         bool
	OpenSearchIndexNamePrefix  string
	OpenSearchSemanticModelID  string
}

func NewOperatorConfig() OperatorConfig {
	return OperatorConfig{
		KCPKubeconfig:              "/api-kubeconfig/kubeconfig",
		APIExportEndpointSliceName: "search.platform-mesh.io",
		OpenSearchIndexNamePrefix:  "pm-orgs",
	}
}

// wrapper for `pflag.Value` interface
type gvkSliceValue struct {
	cfg *OperatorConfig
}

func (g *gvkSliceValue) String() string {
	parts := make([]string, len(g.cfg.SearchableResources))
	for i, gvk := range g.cfg.SearchableResources {
		parts[i] = gvk.Group + "/" + gvk.Version + "/" + gvk.Kind
	}
	return strings.Join(parts, ",")
}

func (g *gvkSliceValue) Set(val string) error {
	for _, s := range strings.Split(val, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		parts := strings.SplitN(s, "/", 3)
		if len(parts) != 3 {
			return fmt.Errorf("invalid GVK %q: expected group/version/kind", s)
		}
		g.cfg.SearchableResources = append(g.cfg.SearchableResources, schema.GroupVersionKind{
			Group:   parts[0],
			Version: parts[1],
			Kind:    parts[2],
		})
	}
	return nil
}

func (g *gvkSliceValue) Type() string { return "gvkSlice" }

func (c *OperatorConfig) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.KCPKubeconfig, "kcp-kubeconfig", c.KCPKubeconfig, "Path to the kcp kubeconfig file")
	fs.StringVar(&c.APIExportEndpointSliceName, "api-export-endpoint-slice-name", c.APIExportEndpointSliceName, "Name of the APIExportEndpointSlice to use for the multicluster provider")
	fs.StringVar(&c.OpenSearchURL, "opensearch-url", c.OpenSearchURL, "OpenSearch server URL (e.g. https://localhost:9200)")
	fs.StringVar(&c.OpenSearchUsername, "opensearch-username", c.OpenSearchUsername, "Username for OpenSearch basic auth")
	fs.StringVar(&c.OpenSearchPassword, "opensearch-password", c.OpenSearchPassword, "Password for OpenSearch basic auth")
	fs.BoolVar(&c.OpenSearchInsecure, "opensearch-insecure", c.OpenSearchInsecure, "Skip TLS certificate verification for OpenSearch (development only)")
	fs.StringVar(&c.OpenSearchIndexNamePrefix, "opensearch-index-name-prefix", c.OpenSearchIndexNamePrefix, "Static prefix for all operator-managed OpenSearch index names and aliases")
	fs.StringVar(&c.OpenSearchSemanticModelID, "opensearch-semantic-model-id", c.OpenSearchSemanticModelID, "OpenSearch ML model ID used for semantic field mappings (optional)")
	fs.Var(&gvkSliceValue{cfg: c}, "searchable-resources", "Comma-separated list of GroupVersionKind values to index, each in group/version/kind format (repeatable)")
}
