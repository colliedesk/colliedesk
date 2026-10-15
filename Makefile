.PHONY: test build e2e dev clean sync-user-docs

GO_ENV := GOPATH=/tmp/colliedesk-gopath GOMODCACHE=/tmp/colliedesk-gopath/pkg/mod GOCACHE=/tmp/colliedesk-go-cache
TEST_DATABASE_URL ?= postgres://colliedesk:colliedesk@127.0.0.1:55441/colliedesk_test?sslmode=disable

test:
	sh tests/mail-pipe-test.sh
	node --test infra/mail/cloud-inbound-worker/worker.test.mjs
	sh tests/install-test.sh
	test -z "$$(gofmt -l server)" || { gofmt -l server; echo 'gofmt: format the files above' >&2; exit 1; }
	cd server && $(GO_ENV) go vet ./...
	cd server && TEST_DATABASE_URL='$(TEST_DATABASE_URL)' $(GO_ENV) go test ./...
	cd web && corepack pnpm typecheck
	cd web && corepack pnpm test
	cd web && corepack pnpm build
	cd web && corepack pnpm bundle:check

build:
	cd web && corepack pnpm build
	mkdir -p .cache
	cd server && $(GO_ENV) go build -o ../.cache/colliedesk ./cmd/colliedesk

# e2e needs PostgreSQL and Mailpit: docker compose --profile test up -d postgres mailpit
# E2E_ARGS selects tests, for example E2E_ARGS='--grep @onboarding'.
e2e:
	cd web && corepack pnpm build
	cd web && TEST_DATABASE_URL='$(TEST_DATABASE_URL)' corepack pnpm e2e $(E2E_ARGS)

# The server embeds a copy of docs/user for its documentation search tool (go:embed cannot reach
# outside the Go module). Run this after editing a guide; a Go test fails when the copy is stale.
sync-user-docs:
	rm -f server/internal/mcpserver/userdocs/*.md
	cp docs/user/*.md server/internal/mcpserver/userdocs/

dev:
	docker compose up --build

clean:
	$(RM) -r .cache server/internal/webui/static/build web/dist
