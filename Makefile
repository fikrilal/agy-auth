.PHONY: build test vet fmt install npm-build npm-pack release clean verify verify-full verify-ci lint doctor hooks

VERSION ?= $(shell node -p "require('./package.json').version" 2>/dev/null || echo 0.1.0)
LDFLAGS := -s -w -X github.com/fikrilal/agy-auth/internal/cli.Version=$(VERSION)
PREFIX ?= $(HOME)/.local
BINARY := bin/agy-auth
HARNESS := go run ./cmd/harness

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agy-auth

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

verify:
	$(HARNESS) verify --profile fast

verify-full:
	$(HARNESS) verify --profile full

verify-ci:
	$(HARNESS) verify --profile ci

lint:
	$(HARNESS) lint

doctor:
	$(HARNESS) doctor

hooks:
	$(HARNESS) install-hooks

install:
	mkdir -p $(PREFIX)/bin
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(PREFIX)/bin/agy-auth ./cmd/agy-auth

npm-build:
	node scripts/build-npm.mjs --all

npm-pack:
	npm pack

release:
	@echo "git tag v$(VERSION) && git push origin v$(VERSION)"

clean:
	rm -rf bin dist npm/vendor *.tgz
