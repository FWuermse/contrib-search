# Per-chart OCM settings

`hack/release/build-aggregate.sh` publishes an OCM component for every chart
under `charts/`. All of them carry the version `V` of the build. The default
shape is

```
github.com/platform-mesh/<chart>:V                      service component
  ├─ chart → github.com/platform-mesh/helm-charts/<chart>:V
  │            resource "chart" (the packaged chart in the OCI registry)
  │            source   "chart" (this repository at the released commit)
  └─ image → github.com/platform-mesh/images/<chart>:<image version>
```

A file `ocm/charts/<chart>.yaml` adjusts the shape for one chart. All keys are
optional:

| Key | Effect |
|---|---|
| `publish: false` | push the chart to the registry, but create no OCM component (library charts) |
| `flat: true` | put the chart resource directly into the service component, no `helm-charts/` layer |
| `image: false` | no `image` reference even though `appVersion` is set |
| `images:` | list of OCM resources (third-party images the chart deploys) |
| `extraReferences:` | additional `componentReferences` of the service component |

Charts without such a file use the defaults.
