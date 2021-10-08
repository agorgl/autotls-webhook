# Build stage
FROM golang:1.17-alpine as build

# Copy the Go Modules manifests
WORKDIR /workspace
COPY go.mod ./
COPY go.sum ./

# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY *.go ./

# Build
RUN CGO_ENABLED=0 go build -a -o app

# Run stage
# Use distroless as minimal base image to package the app binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /workspace/app .
USER 65532:65532

ENTRYPOINT ["/app"]
