FROM node:22-bookworm-slim@sha256:c3de60bf2f9dd0ac6370e6117950ff62d6e339527e7472301c9c78a017978392 AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && corepack install
RUN corepack pnpm install --frozen-lockfile
COPY contracts /src/contracts
# The knowledge base preview reads the help center's HTML allowlist.
COPY server/internal/kbhtml /src/server/internal/kbhtml
COPY web ./
COPY server/internal/webui/static /src/server/internal/webui/static
# Only the Vite build: the e2e job runs the API drift and type checks on the same commit, and here
# they competed for the runner's cores with the Go stage, the image's long pole.
RUN corepack pnpm exec vite build

# Alpine only builds the static binary (CGO_ENABLED=0). Its pull is 75 MB against 302 MB for
# bookworm, whose pull took about 25 seconds in CI while the Go stage waited for it.
FROM golang:1.27-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS go-modules
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download

# The dependency packages the binary imports, listed from the source. Only this list reaches the
# dependency compile below, so a commit that keeps the imports reuses that layer from CI's cache
# (docs/internals/performance.md, CI phase budget).
FROM go-modules AS go-deps
COPY server ./
RUN CGO_ENABLED=0 go list -e -deps -f '{{if and .Module (not .Module.Main)}}{{.ImportPath}}{{end}}' ./cmd/collie > /deps.txt

FROM go-modules AS go
COPY --from=go-deps /deps.txt /deps.txt
# Compile every dependency while the web stage builds; the final build reuses these packages.
RUN CGO_ENABLED=0 go build -trimpath $(cat /deps.txt)
COPY server ./
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
