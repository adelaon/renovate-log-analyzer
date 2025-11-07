# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:latest AS builder

ENV GOTOOLCHAIN=auto

#WORKDIR /workspace

# Copy go mod files
COPY go.mod go.mod

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go main.go

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o renovate-log-analyzer main.go

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
WORKDIR /
# OpenShift preflight check requires licensing files under /licenses
COPY licenses/ licenses

# Copy the binary from builder
COPY --from=builder /opt/app-root/src/renovate-log-analyzer .

# Run as non-root user
USER 65532:65532

ENTRYPOINT ["/renovate-log-analyzer"]
