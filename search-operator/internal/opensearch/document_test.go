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
	"encoding/json"
	"strings"
	"testing"

	pmsearchv1alpha1 "go.platform-mesh.io/search-operator/api/v1alpha1"
)

func TestDefaultIndexMappingIsValidJSON(t *testing.T) {
	mapping, err := DefaultIndexMapping(FieldMappings{}, "")
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}
	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v\nMapping content:\n%s", err, mapping)
	}
}

func mappingProperties(t *testing.T, fields FieldMappings, semanticModelID string) (map[string]any, map[string]any) {
	t.Helper()

	mapping, err := DefaultIndexMapping(fields, semanticModelID)
	if err != nil {
		t.Fatalf("DefaultIndexMapping() returned error: %v", err)
	}

	var js map[string]any
	if err := json.Unmarshal([]byte(mapping), &js); err != nil {
		t.Fatalf("DefaultIndexMapping() returned invalid JSON: %v\nMapping content:\n%s", err, mapping)
	}
	return js, js["properties"].(map[string]any)
}

// leafAt walks object properties along a dot-notation path below a document section.
func leafAt(t *testing.T, properties map[string]any, section, fieldPath string) map[string]any {
	t.Helper()

	node := properties[section].(map[string]any)
	for _, segment := range strings.Split(fieldPath, ".") {
		children, ok := node["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s.%s: %v has no properties", section, fieldPath, node)
		}
		next, ok := children[segment].(map[string]any)
		if !ok {
			t.Fatalf("%s.%s: missing segment %q in %v", section, fieldPath, segment, children)
		}
		node = next
	}
	return node
}

func TestDefaultIndexMappingIncludesSemanticFields(t *testing.T) {
	_, properties := mappingProperties(t, FieldMappings{Semantic: []string{"description", "spec.summary"}}, "model-123")

	for _, fieldPath := range []string{"description", "spec.summary"} {
		leaf := leafAt(t, properties, "semantic_fields", fieldPath)
		//nolint:goconst
		if got := leaf["type"]; got != "semantic" {
			t.Fatalf("%s type = %v, want semantic", fieldPath, got)
		}
		if got := leaf["model_id"]; got != "model-123" {
			t.Fatalf("%s model_id = %v, want model-123", fieldPath, got)
		}
	}
	if _, ok := properties["spec"]; ok {
		t.Fatalf("semantic fields must not be mapped outside semantic_fields")
	}
}

func TestDefaultIndexMappingRequiresSemanticModelID(t *testing.T) {
	if _, err := DefaultIndexMapping(FieldMappings{Semantic: []string{"description"}}, ""); err == nil {
		t.Fatal("DefaultIndexMapping() error = nil, want semantic model id validation error")
	}
}

func TestDefaultIndexMappingIncludesFilterableFields(t *testing.T) {
	_, properties := mappingProperties(t, FieldMappings{Filterable: []string{"status.phase"}}, "")

	if got := leafAt(t, properties, "filterable_fields", "status.phase")["type"]; got != "keyword" {
		t.Fatalf("filterable_fields.status.phase type = %v, want keyword", got)
	}
	if _, ok := properties["status"]; ok {
		t.Fatalf("filterable fields must not be mapped outside filterable_fields")
	}
}

func TestDefaultIndexMappingTypesDefaultFields(t *testing.T) {
	_, properties := mappingProperties(t, FieldMappings{
		Default: []string{"spec.description", "spec.count", "spec.ratio", "spec.enabled", "metadata.creationTimestamp", "status.conditions"},
		DefaultTypes: map[string]pmsearchv1alpha1.FieldType{
			"spec.description":           pmsearchv1alpha1.FieldTypeString,
			"spec.count":                 pmsearchv1alpha1.FieldTypeInteger,
			"spec.ratio":                 pmsearchv1alpha1.FieldTypeNumber,
			"spec.enabled":               pmsearchv1alpha1.FieldTypeBoolean,
			"metadata.creationTimestamp": pmsearchv1alpha1.FieldTypeDate,
		},
	}, "")

	tests := []struct {
		path     string
		osType   string
		sortType string
	}{
		{"spec.description", "text", "keyword"},
		{"spec.count", "long", "long"},
		{"spec.ratio", "double", "double"},
		{"spec.enabled", "boolean", "boolean"},
		{"metadata.creationTimestamp", "date", "date"},
	}
	for _, tt := range tests {
		leaf := leafAt(t, properties, "default_fields", tt.path)
		if got := leaf["type"]; got != tt.osType {
			t.Errorf("default_fields.%s type = %v, want %s", tt.path, got, tt.osType)
		}
		sortField, ok := leaf["fields"].(map[string]any)[SortSubfield].(map[string]any)
		if !ok {
			t.Errorf("default_fields.%s has no %q subfield: %v", tt.path, SortSubfield, leaf)
			continue
		}
		if got := sortField["type"]; got != tt.sortType {
			t.Errorf("default_fields.%s.%s type = %v, want %s", tt.path, SortSubfield, got, tt.sortType)
		}
	}

	if got := leafAt(t, properties, "default_fields", "spec.description")["fields"].(map[string]any)[SortSubfield].(map[string]any)["normalizer"]; got != sortNormalizer {
		t.Errorf("text sort subfield normalizer = %v, want %s", got, sortNormalizer)
	}

	// Untyped paths are left to the dynamic templates.
	status := properties["default_fields"].(map[string]any)["properties"].(map[string]any)
	if _, ok := status["status"]; ok {
		t.Errorf("untyped default field status.conditions must not be mapped explicitly")
	}
	if _, ok := properties["spec"]; ok {
		t.Errorf("default fields must not be mapped outside default_fields")
	}
}

func TestDefaultIndexMappingIndexesUntypedDefaultFieldsAsText(t *testing.T) {
	js, _ := mappingProperties(t, FieldMappings{}, "")

	if got := js["date_detection"]; got != false {
		t.Fatalf("date_detection = %v, want false", got)
	}

	matched := map[string]bool{}
	for _, raw := range js["dynamic_templates"].([]any) {
		for _, tmpl := range raw.(map[string]any) {
			tmpl := tmpl.(map[string]any)
			if tmpl["path_match"] != "default_fields.*" {
				continue
			}
			mapping := tmpl["mapping"].(map[string]any)
			if mapping["type"] != "text" {
				t.Fatalf("dynamic default field mapping = %v, want text", mapping)
			}
			if _, ok := mapping["fields"].(map[string]any)[SortSubfield]; !ok {
				t.Fatalf("dynamic default field mapping has no %q subfield: %v", SortSubfield, mapping)
			}
			matched[tmpl["match_mapping_type"].(string)] = true
		}
	}
	for _, jsonType := range []string{"string", "long", "double", "boolean"} {
		if !matched[jsonType] {
			t.Errorf("no dynamic template maps %s default field values as text", jsonType)
		}
	}
}

func TestDefaultIndexMappingKeepsSectionsApart(t *testing.T) {
	// The same path may appear in every list; each section maps it on its own.
	_, properties := mappingProperties(t, FieldMappings{
		Default:      []string{"spec.title"},
		DefaultTypes: map[string]pmsearchv1alpha1.FieldType{"spec.title": pmsearchv1alpha1.FieldTypeString},
		Semantic:     []string{"spec.title"},
		Filterable:   []string{"spec.title"},
	}, "model-123")

	for section, want := range map[string]string{"default_fields": "text", "semantic_fields": "semantic", "filterable_fields": "keyword"} {
		if got := leafAt(t, properties, section, "spec.title")["type"]; got != want {
			t.Errorf("%s.spec.title type = %v, want %s", section, got, want)
		}
	}
}

func TestSetDefaultFilterableFieldsAllocatesNilMap(t *testing.T) {
	doc := &ResourceDocument{
		Kind:          "ConfigMap",
		Name:          "my-cm",
		Namespace:     "default",
		ClusterName:   "cluster-1",
		WorkspacePath: "root:org:ws",
	}

	doc.SetDefaultFilterableFields()

	want := map[string]any{
		"kind":           "ConfigMap",
		"name":           "my-cm",
		"namespace":      "default",
		"cluster_name":   "cluster-1",
		"workspace_path": "root:org:ws",
	}
	for k, v := range want {
		if got := doc.FilterableFields[k]; got != v {
			t.Errorf("FilterableFields[%q] = %v, want %v", k, got, v)
		}
	}
}

func TestSetDefaultFilterableFieldsPreservesConfiguredFields(t *testing.T) {
	doc := &ResourceDocument{
		Kind: "ConfigMap",
		FilterableFields: map[string]any{
			"status.phase": "Ready",
		},
	}

	doc.SetDefaultFilterableFields()

	if got := doc.FilterableFields["status.phase"]; got != "Ready" {
		t.Errorf("configured field status.phase = %v, want Ready", got)
	}
	if got := doc.FilterableFields["kind"]; got != "ConfigMap" {
		t.Errorf("FilterableFields[kind] = %v, want ConfigMap", got)
	}
}
