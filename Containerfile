# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:latest AS builder

ARG TARGETOS
ARG TARGETARCH
ENV GOTOOLCHAIN=auto

# Copy the Go Modules manifests
COPY go.mod go.mod
# cache deps before building and copying source
RUN go mod download

# Copy the source code
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build the binary
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -a -o renovate-log-analyzer cmd/log-analyzer/main.go

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
WORKDIR /
# OpenShift preflight check requires licensing files under /licenses
COPY licenses/ licenses

# Copy the binary from builder
COPY --from=builder /opt/app-root/src/renovate-log-analyzer .

# Labels
LABEL name="Renovate Log Analyzer"
LABEL description="Log analysis and webhook service for Mintmaker-Renovate"
LABEL io.k8s.description="Renovate Log Analyzer"
LABEL io.k8s.display-name="renovate-log-analyzer"
LABEL summary="Renovate Log Analyzer"
LABEL com.redhat.component="renovate-log-analyzer"

# Run as non-root user
USER 65532:65532

ENTRYPOINT ["/renovate-log-analyzer"]