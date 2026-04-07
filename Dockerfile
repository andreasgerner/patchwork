FROM golang:1.23.4 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY main.go main.go
COPY api/ api/
COPY internal/ internal/
COPY pkg/ pkg/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w \
      -X github.com/andreasgerner/patchwork/pkg/version.Version=${VERSION} \
      -X github.com/andreasgerner/patchwork/pkg/version.GitCommit=${GIT_COMMIT}" \
    -a -o patchwork .

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/patchwork .
USER 65532:65532

ENTRYPOINT ["/patchwork"]
