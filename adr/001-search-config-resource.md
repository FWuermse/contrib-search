# ADR 007: SearchConfig Resource for Provider-Defined Field Indexing

| Status  | Proposed   |
|---------|------------|
| Date    | 2026-04-30 |

## Context and Problem Statement

The search-operator (RFC-003) currently auto-populates field classification on `SearchIndex` resources by reading the `APIResourceSchema` and applying heuristics: all top-level fields become `defaultFields`, all `string`-typed fields become `semanticFields`, and all scalar-typed fields become `filterableFields`. This one-size-fits-all approach has several problems:

1. **Providers have no control over indexing behavior.** A provider that exposes large text blobs (e.g., base64-encoded certificates, raw JSON payloads) has those fields indexed as semantic — wasting vector storage and degrading search quality.
2. **No way to exclude fields.** Internal identifiers, secrets, or denormalized cache fields are indexed even when they provide no search value.
3. **Heuristics cannot capture domain intent.** Only the provider team knows that `spec.displayName` is semantically meaningful while `spec.internalRef` is an opaque ID useful only for exact-match filtering.
4. **No service contract between provider and search.** If a provider changes its schema, the search behavior changes silently. There is no explicit declaration of indexing intent.

RFC-004 ("Everything is a Provider") establishes that providers should own their configuration and expose it via APIExports. The search provider's field classification should follow this model: the **resource provider** declares how its fields should be indexed, and the search-operator honors that declaration.

## Decision Drivers

* **Provider autonomy**: Providers must control how their resources appear in search without requiring search-operator code changes.
* **Explicit over implicit**: Field classification should be declared, not inferred.
* **Service contract**: The SearchConfig acts as a contract between the resource provider and the search provider.
* **Co-location**: Configuration lives alongside the APIResourceSchema it describes, in the provider workspace.
* **Backwards compatibility**: Providers that do not supply a SearchConfig get the current auto-populated behavior (heuristic fallback).

## Decision

Introduce a new `SearchConfig` APIResourceSchema (`search.platform-mesh.io/v1alpha1`) that resource providers create in their provider workspace to declare per-APIResourceSchema field indexing behavior.

### SearchConfig Resource Definition

```yaml
apiVersion: search.platform-mesh.io/v1alpha1
kind: SearchConfig
metadata:
  name: accounts.core.platform-mesh.io  # matches the APIResourceSchema name
spec:
  # Reference to the APIResourceSchema this config applies to
  apiResourceSchemaRef:
    name: accounts.core.platform-mesh.io

  # Fields that should NOT be indexed at all (excluded from defaultFields, semantic, and filterable)
  excludedFields:
    - spec.internalCacheJSON
    - spec.certificateBundle
    - status.reconciledGeneration

  # Fields that should be indexed for semantic/vector search
  # These are typically human-readable text fields where meaning matters
  semanticFields:
    - spec.displayName
    - spec.description
    - spec.tags

  # Fields that should be indexed as exact-match (keyword) for filtering/faceting
  # These are typically identifiers, enums, or structured values
  exactFields:
    - spec.region
    - spec.tier
    - status.phase
    - metadata.labels

status:
  conditions:
    - type: Ready
      status: "True"
    - type: SchemaValid
      status: "True"
      message: "All referenced fields exist in APIResourceSchema"
```

### Field Classification Priority

When the search-operator builds the `SearchIndex` field lists, it applies this priority:

1. **excludedFields**: removed from all index field lists. Never indexed.
2. **exactFields**: indexed as `keyword` type only. Not full-text, not semantic.
3. **semanticFields**: indexed as `semantic` type (vector embeddings). Also included in full-text.

This means a provider only needs to declare the **exceptions** to the default full-text behavior.

### Where SearchConfig Lives

```
:root:providers:<provider-name>
├── APIExport: <provider>.platform-mesh.io
├── APIResourceSchema: widgets.acme.example.io    ← defines the resource fields
└── SearchConfig: widgets.acme.example.io         ← declares how those fields are indexed
```

The SearchConfig resource is stored in the **provider workspace**, alongside the APIResourceSchema it configures. This follows the pattern established in RFC-004 where provider configuration is co-located with provider resources.

#### SearchConfig Binding in Provider Workspaces

To create `SearchConfig` objects in a provider workspace, that workspace must have `search.platform-mesh.io` bound (so the `SearchConfig` CRD is available). This raises a concern: the search-operator watches APIBindings to discover which workspaces to index — could binding `search.platform-mesh.io` in a provider workspace accidentally cause it to be treated as a consumer workspace?

This conflict does not arise because the search-operator only indexes resources in **org workspaces** (under `:root:orgs`). The `APIBindingReconciler` resolves the org workspace from the binding's `status.APIExportClusterName` and creates `SearchIndex` resources there. Provider workspaces are never the target of indexing — they are only read as the source of `SearchConfig` and `APIResourceSchema` objects via the export client.

To make `SearchConfig` objects available for creation in a provider workspace, the provider workspace binds `search.platform-mesh.io` accepting only the `logicalclusters` claim (required for KCP workspace access). This is a minimal binding that grants CRD availability without triggering any indexing behavior.

### How the Search-Operator Reads SearchConfig

The `APIBindingReconciler` is extended with a new step:

```
APIBinding reaches Bound phase in consumer workspace
  │
  ▼
1. Resolve provider workspace (binding.Status.APIExportClusterName)
  │
  ▼
2. For each bound resource, fetch APIResourceSchema from provider workspace
  │
  ▼
3. NEW: Check if a SearchConfig exists in provider workspace
   with name matching the APIResourceSchema name
  │
  ├── SearchConfig EXISTS → use declared field classification
  │     • excludedFields → remove from all lists
  │     • semanticFields → SearchIndex.spec.semanticFields
  │     • exactFields → SearchIndex.spec.filterableFields
  │
  ├── SearchConfig DOES NOT EXIST → index every fields as defaultField
  │
  ▼
4. Create/update SearchIndex in org workspace (unchanged)
```

### KCP Workspace Topology & Dependencies

```
:root
├── :providers
│   ├── :search                          ← Search provider (owns SearchConfig APIResourceSchema)
│   │   ├── APIExport: search.platform-mesh.io
│   │   │   └── permissionClaims: [accounts, searchindexes, searchconfigs, ...]
│   │   └── APIResourceSchema: searchconfigs.search.platform-mesh.io
│   │
│   ├── :core                            ← Core provider (produces SearchConfig for its resources)
│   │   ├── APIExport: core.platform-mesh.io
│   │   ├── APIResourceSchema: accounts.core.platform-mesh.io
│   │   └── SearchConfig: accounts.core.platform-mesh.io  ← "index my fields like this"
│   │
│   └── :acme                            ← 3rd-party provider
│       ├── APIExport: acme.example.io
│       ├── APIResourceSchema: widgets.acme.example.io
│       └── SearchConfig: widgets.acme.example.io
│
├── :orgs
│   └── :acme-org (cluster: org-123)
│       ├── SearchIndex: pm-org-123-accounts     ← fields derived from SearchConfig
│       ├── SearchIndex: pm-org-123-widgets      ← fields derived from SearchConfig
│       │
│       └── :dev-team (account workspace)
│           ├── APIBinding → core.platform-mesh.io
│           ├── APIBinding → acme.example.io
│           └── Account / Widget resources (indexed per SearchIndex config)
│
└── :platform-mesh-system
```

**Dependency flow:**

```
Provider creates SearchConfig ──► search-operator reads it ──► SearchIndex spec populated ──► OpenSearch mapping created
         (provider ws)                  (via provider client)        (org workspace)              (OpenSearch cluster)
```

### Permission Claims

The search-operator's APIExport must add a permission claim for `searchconfigs`:

```yaml
apiVersion: apis.kcp.io/v1alpha1
kind: APIExport
metadata:
  name: search.platform-mesh.io
spec:
  permissionClaims:
    - group: search.platform-mesh.io
      resource: searchconfigs
      all: true    # read SearchConfig from all provider workspaces
    # ... existing claims
```

### Validation

The search-operator validates SearchConfig on read:

1. **Schema cross-reference**: All field paths in `excludedFields`, `semanticFields`, and `exactFields` must exist in the referenced APIResourceSchema. If a field is referenced but does not exist, set `status.conditions[SchemaValid]=False`.
2. **No overlap**: A field cannot appear in more than one list. If overlap is detected, the highest-priority list wins (`excludedFields` > `exactFields` > `semanticFields`).
3. **Semantic field type check**: The search-operator does not validate field types against the APIResourceSchema. The value at the declared path is passed as-is to the embedding model, which requires text input. Providers are responsible for only declaring fields whose runtime values produce meaningful text. This includes plain strings, but also object subtrees or arrays whose content is text-based.

## Consequences

* Good, because providers gain explicit control over how their resources are indexed without search-operator code changes.
* Good, because the SearchConfig acts as a versioned, auditable service contract between resource providers and the search platform.
* Good, because excluding sensitive or large fields prevents index bloat and protects data that should not be searchable.
* Good, because the heuristic fallback ensures backwards compatibility — no provider is forced to create a SearchConfig.
* Good, because SearchConfig is co-located with the APIResourceSchema it describes, making consistency easy to maintain.
* Bad, because providers must now maintain an additional resource alongside their APIResourceSchema.
* Bad, because the search-operator now reads from provider workspaces (additional cross-workspace client), increasing reconciliation complexity.
* Bad, because schema changes in APIResourceSchema may invalidate SearchConfig field references, requiring providers to keep them in sync.

## Alternatives Considered

### 1. Annotations on APIResourceSchema fields

Encode indexing hints as annotations directly on the APIResourceSchema OpenAPI schema (e.g., `x-search-semantic: true`). Rejected because:
- KCP does not support custom extensions in APIResourceSchema validation schemas
- Mixes concerns (API definition vs. search configuration)
- Not discoverable or auditable as a standalone resource

### 2. SearchIndex-level overrides by org admins

Allow org admins to override field classification on the SearchIndex in their org workspace. Rejected because:
- Org admins lack domain knowledge about which fields are semantically meaningful
- Creates drift between organizations consuming the same provider
- Does not establish a service contract

### 3. Configuration in the search-operator environment variables

Extend `SEARCHABLE_RESOURCE_RESOURCES` env var format to include field config. Rejected because:
- Requires search-operator redeployment for any field change
- Not provider-self-service
- Does not scale with number of providers

## References

- [RFC 003: Search Architecture](../rfc/003-search-architecture.md)
- [RFC 004: Core Platform Extendability](../rfc/004_core-platform-extendability.md)
- [RFC 006: Provider Bootstrap Operator](../rfc/006_provider-bootstrap-operator.md)
- [ADR 002: APIExport Binding Access Control](002-apiexport-binding-access-control.md)
- [kcp APIResourceSchema documentation](https://docs.kcp.io/kcp/main/concepts/apis/)
