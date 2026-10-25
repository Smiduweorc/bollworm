BINARY  := bollworm
PKG     := ./cmd/bollworm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := build

## setup: install the git hooks and the commit/changelog tooling (npm), safe to rerun
.PHONY: setup
setup:
	go mod download
	npm ci

## fmt: format Go sources in place (given FILES=..., only those)
.PHONY: fmt
fmt:
	gofmt -w $(or $(FILES),.)

SHELL_SCRIPTS := install.sh $(wildcard scripts/*.sh)

## lint: gofmt check, go vet, golangci-lint, go.mod tidiness, shellcheck; changes nothing
.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not found. Install it: pacman -S golangci-lint, or see https://golangci-lint.run/docs/welcome/install/"; exit 1; }
	@command -v shellcheck >/dev/null || { echo "shellcheck not found. Install it: pacman -S shellcheck, or apt-get install shellcheck"; exit 1; }
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	golangci-lint run ./...
	go mod tidy -diff
	shellcheck $(SHELL_SCRIPTS)
	sh -n install.sh
	for f in $(wildcard scripts/*.sh); do bash -n "$$f" || exit 1; done

## go-version: fail if go.mod is behind the latest Go release (needs network, skips offline)
.PHONY: go-version
go-version:
	scripts/check-go-version.sh

## test: the full suite with the race detector
.PHONY: test
test:
	go test -race -coverprofile=coverage.out ./...

## fuzz: fuzz the parser and renderer (FUZZTIME=30s by default)
.PHONY: fuzz
fuzz:
	go test ./internal/render -run '^$$' -fuzz FuzzRender -fuzztime $(or $(FUZZTIME),30s)

## validate: check rendered documents against the ECMA-376 schema (needs curl, xmllint)
.PHONY: validate
validate:
	scripts/validate-ooxml.sh

## build: static, zero-cgo binary
.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

## preflight: lint then test, exactly what release runs first
.PHONY: preflight
preflight: lint test

## changelog: regenerate CHANGELOG.md from the commit history
.PHONY: changelog
changelog:
	git-cliff -o CHANGELOG.md

## release: cut a release, V=vX.Y.Z or V=patch|minor|major (PUSH=1 to push)
.PHONY: release
release:
	@test -n "$(V)" || (echo "set V=<vX.Y.Z|major|minor|patch>, for example: make release V=patch" && exit 1)
	scripts/release.sh $(V)

## snapshot: local GoReleaser build, publishes nothing
.PHONY: snapshot
snapshot:
	goreleaser release --snapshot --clean

## hooks: (re)install the git hooks
.PHONY: hooks
hooks:
	npx lefthook install

## clean: remove build artifacts
.PHONY: clean
clean:
	rm -rf $(BINARY) dist build coverage.out

## help: list the tasks
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
