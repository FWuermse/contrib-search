# ADR 003: Per-Account Resource Counts on Indexed Account Documents

| Status  | Proposed   |
|---------|------------|
| Date    | 2026-05-14 |

## Context and Problem Statement

The search-operator indexes a configurable set of kcp resources (`searchable-resources`) into OpenSearch, one `IndexableResource` reconciler per type. Account and Organization resources are also indexed, as documents in the `accounts` index.

Users want to filter and sort accounts by how much of a resource type they hold (e.g. "accounts with the most `Binding`s"). This is not possible today: the count of a type lives in the account's own workspace, the Account document is derived from the parent's `AccountInfo`, and OpenSearch cannot join across documents at query time. The count must therefore be denormalized onto the Account document and kept current as resources are created and deleted.

## Decision

When an `IndexableResource` reconciler processes a resource of a counted type, it resolves the account workspace that resource lives in, counts the resources of that type in that workspace, and writes the count onto that Account's document.

### Where the count lives

A `resource_counts` map under the Account document's existing `filterable_fields`, keyed by resource plural:

```jsonc
"filterable_fields": {
  "resource_counts": { "bindings": 12, "stores": 3 }
}
```

Reusing `filterable_fields` means it is indexed as a facet with no new caller-facing schema, and each type gets its own key.

### Producing the count

The account workspace is the one the reconciled resource lives in, resolved from the account linkage already computed for every document (`cluster_name` / `AccountInfo`). The operator counts the type in that single workspace (not the wildcard virtual workspace) and partial-updates `filterable_fields.resource_counts.<plural>`.

The count is recomputed on every reconcile of the type (deletes run in `Finalize`), so it is self-correcting — no `+1`/`-1` bookkeeping that can drift.

### How to count via the Kubernetes API

An account may hold many resources of a type, so how we count matters. Three options, all available through the controller-runtime client already in use:

| Option | Transferred | Cost |
|--------|-------------|------|
| Normal `List` | Full objects (spec/status/managedFields) | O(N), full decode |
| `PartialObjectMetadataList` | Metadata only, one entry per object | O(N) items |
| Metadata-only, `Limit: 1`, `remainingItemCount` | One item + an int | O(1) |

**Decision: metadata-only list with `Limit: 1`, count = `len(items) + remainingItemCount`.** This keeps the per-reconcile read cost constant regardless of workspace size.

```go
list := &metav1.PartialObjectMetadataList{}
list.SetGroupVersionKind(gvk)
if err := wsClient.List(ctx, list, client.Limit(1)); err != nil { /* ... */ }
count := int64(len(list.Items))
if rem := list.GetRemainingItemCount(); rem != nil {
    count += *rem
}
```

`remainingItemCount` is a best-effort hint and may be `nil` (e.g. cache-served or filtered lists); when it is, fall back to counting returned items with a full metadata-only list.

### Missing Account and deletion

If the target Account document does not exist yet (indexed later, or incomplete `AccountInfo`), the reconcile requeues using the existing guard; counts are never written as a standalone document. On deletion, `Finalize` recomputes and rewrites the count (dropping to `0` when the last of a type is removed).

## Consequences

* Good: accounts become filterable/sortable by resource counts directly in OpenSearch, with no cross-document joins.
* Good: produced by the existing reconcilers and account linkage — no new reconciler or provider contract.
* Good: recomputation is self-correcting; the metadata-only `Limit: 1` read keeps cost constant per reconcile.
* Bad: every reconcile of a counted resource adds a count read and an Account-document write, so high churn causes write amplification on the `accounts` index.
* Bad: only resources the operator can see through the search APIExport virtual workspace are counted.

## Alternatives Considered

* **Query-time aggregation** instead of denormalizing: rejected — still needs a join back to Account documents (which OpenSearch cannot do in one query) and scales with indexed-resource count.
* **Incremental `+1`/`-1`** scripted updates: rejected — missed/duplicated events skew the count permanently with no self-healing.
* **Dedicated CountReconciler** watching account workspaces: rejected for now — duplicates account resolution already in the indexing reconcilers; viable later if per-event recomputation proves too noisy.

## References

- [ADR 001: SearchConfig Resource for Provider-Defined Field Indexing](001-search-config-resource.md)
- [ADR 002: Search Implementation in Platform Mesh using OpenSearch and Authorization](002-search-authorization.md)
- [RFC 001: Search Architecture](../rfc/001-search-architecture.md)
