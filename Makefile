.PHONY: test build e2e dev clean

GO_ENV := GOPATH=/tmp/colliedesk-gopath GOMODCACHE=/tmp/colliedesk-gopath/pkg/mod GOCACHE=/tmp/colliedesk-go-cache
TEST_DATABASE_URL ?= postgres://colliedesk:colliedesk@127.0.0.1:55441/colliedesk_test?sslmode=disable

test:
	sh tests/mail-pipe-test.sh
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

e2e:
	cd web && corepack pnpm build
	cd web && TEST_DATABASE_URL='$(TEST_DATABASE_URL)' corepack pnpm e2e

dev:
	docker compose up --build

clean:
	$(RM) -r .cache server/internal/webui/static/build web/dist
