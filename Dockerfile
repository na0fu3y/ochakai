# Reproducible static build: no CGO, trimmed paths, minimal base image.
# Cross-compiles on the build platform (no QEMU emulation for arm64).
# Requires BuildKit: on the legacy builder (plain gcr.io/cloud-builders/docker
# on Cloud Build, or docker without DOCKER_BUILDKIT=1) the next line fails
# with `failed to parse platform : "" is an invalid component`.
#
# Both bases carry a digest as well as a tag. A tag is a name its owner can
# repoint, so the same commit built twice is not necessarily the same
# image; the digest is what makes "this Dockerfile" and "that image" the
# same statement. The tag stays beside it because it is what a reader
# recognises, and because dependabot's docker ecosystem reads the pair and
# opens the pull request that moves both — the digest below is a value
# somebody has to bump, and the point is that bumping it is a commit.
FROM --platform=$BUILDPLATFORM golang:1.27.4@sha256:5f1b98fa02fe396ebce2a0227a0e08309e11792915b9dd2e1f71053d7e7e6666 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /ochakai ./cmd/ochakai

# distroless static: no shell, no package manager, minimal supply chain.
# The Debian version is named rather than left off: distroless resolves a
# bare tag to whatever it currently considers newest, which would move
# this image's base on a day nobody chose. debian13 is where that default
# points today, and the digest holds it still even there — `nonroot` is
# rebuilt in place, so the tag alone names a moving target.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=build /ochakai /ochakai
USER nonroot
ENTRYPOINT ["/ochakai"]
CMD ["serve"]
