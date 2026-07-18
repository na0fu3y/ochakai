# Reproducible static build: no CGO, trimmed paths, minimal base image.
# Cross-compiles on the build platform (no QEMU emulation for arm64).
FROM --platform=$BUILDPLATFORM golang:1.26.4 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /ochakai ./cmd/ochakai

# distroless static: no shell, no package manager, minimal supply chain.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /ochakai /ochakai
USER nonroot
ENTRYPOINT ["/ochakai"]
CMD ["serve"]
