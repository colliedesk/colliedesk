FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && corepack install
RUN corepack pnpm install --frozen-lockfile
COPY contracts /src/contracts
COPY web ./
COPY server/internal/webui/static /src/server/internal/webui/static
RUN corepack pnpm build

FROM golang:1.26-bookworm AS go
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server ./
COPY --from=web /src/server/internal/webui/static/build ./internal/webui/static/build
RUN CGO_ENABLED=0 go build -trimpath -o /out/colliedesk ./cmd/colliedesk

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=go /out/colliedesk /app/colliedesk
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/colliedesk"]
