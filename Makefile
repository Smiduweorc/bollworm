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

## lint: gofmt check, go vet, go.mod tidiness; changes nothing
.PHONY: lint
lint:
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go mod tidy -diff

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
