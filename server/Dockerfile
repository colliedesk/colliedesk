FROM node:22-bookworm-slim@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392 AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && corepack install
RUN corepack pnpm install --frozen-lockfile
COPY contracts /src/contracts
COPY web ./
COPY server/internal/webui/static /src/server/internal/webui/static
RUN corepack pnpm build

FROM golang:1.27-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS go
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
# Compile every dependency while the web stage builds; the final build reuses these packages.
RUN CGO_ENABLED=0 go build -trimpath $(go list -e -deps -f '{{if and .Module (not .Module.Main)}}{{.ImportPath}}{{end}}' ./cmd/collie)
COPY --from=web /src/server/internal/webui/static/build ./internal/webui/static/build
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=${VERSION}" -o /out/collie ./cmd/collie

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=go /out/collie /app/collie
RUN mkdir -p /data/attachments && chown -R 65532:65532 /data
ENV ATTACHMENTS_DIR=/data/attachments
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/collie"]
