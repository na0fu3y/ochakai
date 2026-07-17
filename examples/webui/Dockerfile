# Sample web UI as its own Cloud Run service (kept out of the ochakai
# core image by design). Build from the repository root:
#   docker build -f examples/webui/Dockerfile -t ochakai-webui .
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY examples/webui/ examples/webui/
COPY internal/webui/ internal/webui/
RUN CGO_ENABLED=0 go build -trimpath -o /webui ./examples/webui

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /webui /webui
USER nonroot
ENTRYPOINT ["/webui"]
