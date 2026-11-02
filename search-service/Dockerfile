# Copyright The Platform Mesh Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Build the manager binary.
#
# NOTE: the build context is the repository ROOT, not this directory.
#   docker build -f search-service/Dockerfile .
FROM --platform=$BUILDPLATFORM golang:1.26.7@sha256:dc2521c2a906db43073b8b4d99f491b6341cf15610b6ebbab187c45153f9959e AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY search-service/ search-service/

WORKDIR /workspace/search-service

RUN go mod download

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags '-w -s' -o /workspace/manager main.go

FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
