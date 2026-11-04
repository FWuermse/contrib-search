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

package subroutine

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"testing"

	pmsearchv1alpha1 "go.platform-mesh.io/search-operator/api/v1alpha1"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kcpapisv1alpha1 "github.com/kcp-dev/sdk/apis/apis/v1alpha1"
)

func objectSchema(props map[string]apiextensionsv1.JSONSchemaProps) apiextensionsv1.JSONSchemaProps {
	return apiextensionsv1.JSONSchemaProps{Type: "object", Properties: props}
}

func TestCollectLeafFieldsNested(t *testing.T) {
	root := objectSchema(map[string]apiextensionsv1.JSONSchemaProps{
		"metadata": objectSchema(map[string]apiextensionsv1.JSONSchemaProps{
			"name":              {Type: "string"},
			"creationTimestamp": {Type: "string", Format: "date-time"},
		}),
		"spec": objectSchema(map[string]apiextensionsv1.JSONSchemaProps{
			"description": {Type: "string"},
			"count":       {Type: "integer"},
			"ratio":       {Type: "number"},
			"enabled":     {Type: "boolean"},
			"port":        {XIntOrString: true, AnyOf: []apiextensionsv1.JSONSchemaProps{{Type: "integer"}, {Type: "string"}}},
			"tags": {
				Type:  "array",
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{Schema: &apiextensionsv1.JSONSchemaProps{Type: "string"}},
			},
		}),
		"status": objectSchema(map[string]apiextensionsv1.JSONSchemaProps{
			// array node: emitted as a single leaf, NOT descended into
			"conditions": {
				Type: "array",
				Items: &apiextensionsv1.JSONSchemaPropsOrArray{
					Schema: &apiextensionsv1.JSONSchemaProps{
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{
							"type": {Type: "string"},
						},
					},
				},
			},
			// map-typed node (additionalProperties, no Properties): emitted as a leaf
			"labels": {
				Type:                 "object",
				AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{Allows: true},
			},
		}),
	})

	got := collectLeafFields(&root)

	want := map[string]pmsearchv1alpha1.FieldType{
		"metadata.creationTimestamp": pmsearchv1alpha1.FieldTypeDate,
		"metadata.name":              pmsearchv1alpha1.FieldTypeString,
		"spec.count":                 pmsearchv1alpha1.FieldTypeInteger,
		"spec.description":           pmsearchv1alpha1.FieldTypeString,
		"spec.enabled":               pmsearchv1alpha1.FieldTypeBoolean,
		"spec.port":                  pmsearchv1alpha1.FieldTypeString,
		"spec.ratio":                 pmsearchv1alpha1.FieldTypeNumber,
		"spec.tags":                  pmsearchv1alpha1.FieldTypeString,
		"status.conditions":          "",
		"status.labels":              "",
	}
	if !maps.Equal(got, want) {
		t.Fatalf("collectLeafFields() = %v, want %v", got, want)
	}
}

func TestCollectLeafFieldsDepthGuard(t *testing.T) {
	// Build a chain deeper than maxSchemaWalkDepth; the walk must terminate and emit the
	// node reached at the depth limit as an untyped leaf rather than recursing forever.
	leaf := apiextensionsv1.JSONSchemaProps{Type: "string"}
	node := objectSchema(map[string]apiextensionsv1.JSONSchemaProps{"end": leaf})
	for range maxSchemaWalkDepth + 5 {
		node = objectSchema(map[string]apiextensionsv1.JSONSchemaProps{"child": node})
	}

	got := collectLeafFields(&node)
	if len(got) != 1 {
		t.Fatalf("collectLeafFields() returned %d paths, want 1: %v", len(got), got)
	}
	for path, fieldType := range got {
		if fieldType != "" {
			t.Fatalf("truncated object %q typed as %q, want untyped", path, fieldType)
		}
	}
}

func newFilter(excluded, semantic, exact []string) searchConfigFilter {
	return searchConfigFilter{
		excluded: sets.New[string](excluded...),
		semantic: sets.New[string](semantic...),
		exact:    sets.New[string](exact...),
	}
}

func TestSortIntoAnyOfClassification(t *testing.T) {
	f := newFilter(
		[]string{"metadata.name"},    // excluded
		[]string{"spec.description"}, // semantic
		[]string{"status.phase"},     // exact -> filterable
	)

	defaults := sets.New[string]()
	semantics := sets.New[string]()
	exacts := sets.New[string]()

	for _, p := range []string{"metadata.name", "spec.description", "status.phase", "spec.count"} {
		f.sortIntoAnyOf(p, defaults, semantics, exacts)
	}

	if defaults.Has("metadata.name") {
		t.Fatal("excluded field metadata.name should not appear in any set")
	}
	if semantics.Has("metadata.name") {
		t.Fatal("excluded field metadata.name leaked into semantics")
	}
	if !semantics.Has("spec.description") {
		t.Fatalf("spec.description should be semantic, semantics=%v", semantics)
	}
	if !exacts.Has("status.phase") {
		t.Fatalf("status.phase should be exact/filterable, exacts=%v", exacts)
	}
	if !defaults.Has("spec.count") {
		t.Fatalf("spec.count should be default, defaults=%v", defaults)
	}
}

// TestSortIntoAnyOfArgOrderRegression guards the previously-swapped semantic/exact arguments:
// a semantic field must land in the semantic set (not exact) and vice versa.
func TestSortIntoAnyOfArgOrderRegression(t *testing.T) {
	f := newFilter(nil, []string{"a"}, []string{"b"})

	defaults := sets.New[string]()
	semantics := sets.New[string]()
	exacts := sets.New[string]()

	f.sortIntoAnyOf("a", defaults, semantics, exacts)
	f.sortIntoAnyOf("b", defaults, semantics, exacts)

	if !semantics.Has("a") {
		t.Fatalf("semantic field a not in semantics: %v", semantics)
	}
	if exacts.Has("a") {
		t.Fatal("semantic field a wrongly landed in exacts")
	}
	if !exacts.Has("b") {
		t.Fatalf("exact field b not in exacts: %v", exacts)
	}
	if semantics.Has("b") {
		t.Fatal("exact field b wrongly landed in semantics")
	}
}

func TestResolveFieldsForSchemaTypesDefaultFields(t *testing.T) {
	version := func(name string, specProps map[string]apiextensionsv1.JSONSchemaProps) kcpapisv1alpha1.APIResourceVersion {
		raw, err := json.Marshal(objectSchema(map[string]apiextensionsv1.JSONSchemaProps{"spec": objectSchema(specProps)}))
		if err != nil {
			t.Fatalf("marshal schema: %v", err)
		}
		return kcpapisv1alpha1.APIResourceVersion{Name: name, Served: true, Schema: runtime.RawExtension{Raw: raw}}
	}
	rs := &kcpapisv1alpha1.APIResourceSchema{
		ObjectMeta: metav1.ObjectMeta{Name: "v1.components.example.io"},
		Spec: kcpapisv1alpha1.APIResourceSchemaSpec{Versions: []kcpapisv1alpha1.APIResourceVersion{
			version("v1", map[string]apiextensionsv1.JSONSchemaProps{
				"stars":   {Type: "integer"},
				"title":   {Type: "string"},
				"changed": {Type: "string"},
				"extra":   {Type: "object", AdditionalProperties: &apiextensionsv1.JSONSchemaPropsOrBool{Allows: true}},
			}),
			version("v2", map[string]apiextensionsv1.JSONSchemaProps{
				"stars":   {Type: "integer"},
				"title":   {Type: "string"},
				"changed": {Type: "integer"},
			}),
		}},
	}

	// Without a registered SearchConfig every schema field is a default field.
	pc := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	fields, err := (&apiBindingWatcherSubroutine{}).resolveFieldsForSchema(context.Background(), pc, rs)
	if err != nil {
		t.Fatalf("resolveFieldsForSchema() returned error: %v", err)
	}

	if want := []string{"spec.changed", "spec.extra", "spec.stars", "spec.title"}; !slices.Equal(fields.defaultFields, want) {
		t.Fatalf("defaultFields = %v, want %v", fields.defaultFields, want)
	}
	// Versions disagreeing on spec.changed leave it untyped, like the map spec.extra.
	want := map[string]pmsearchv1alpha1.FieldType{
		"spec.stars": pmsearchv1alpha1.FieldTypeInteger,
		"spec.title": pmsearchv1alpha1.FieldTypeString,
	}
	if !maps.Equal(fields.defaultFieldTypes, want) {
		t.Fatalf("defaultFieldTypes = %v, want %v", fields.defaultFieldTypes, want)
	}
}
