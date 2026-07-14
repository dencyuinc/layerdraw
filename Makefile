# SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

SHELL := /usr/bin/env bash

GO ?= go
PNPM ?= corepack pnpm
GOFMT ?= $(shell $(GO) env GOROOT)/bin/gofmt
VERSION ?= 0.0.0-dev
SOURCE_REVISION ?= $(shell git rev-parse HEAD 2>/dev/null || printf 'unknown')
RELEASE_MANIFEST ?= deploy/development-release-manifest.json
RELEASE_MANIFEST_DIGEST ?= sha256:$(shell { command -v sha256sum >/dev/null && sha256sum $(RELEASE_MANIFEST) || shasum -a 256 $(RELEASE_MANIFEST); } | awk '{print $$1}')
COVERAGE_BASE_REF ?= origin/main
ENGINE_BINARY := dist/layerdraw-engine
ENGINE_WASM_DIR := dist/engine-wasm
LICENSE_REPORT := reports/dependency-licenses.json
GO_PACKAGES := ./cmd/... ./internal/... ./tools/protocolgen

.DEFAULT_GOAL := build

.PHONY: bootstrap generate generate-check format format-check lint typecheck test coverage coverage-check license-check license-report security \
	conformance integration build engine-wasm engine-wasm-check engine-wasm-reproducible protocol-package-check package verify-packaged ci clean

bootstrap:
	$(GO) mod download all
	$(PNPM) install --frozen-lockfile

generate:
	$(GO) run ./tools/protocolgen generate
	$(GO) run ./tools/wasmparity -output tests/conformance/testdata/engine_compile_parity_v1.json
	$(GO) generate ./...
	$(PNPM) exec turbo run generate

generate-check:
	./tools/check-generated.sh --self-test
	./tools/check-generated.sh $(MAKE) generate

format:
	@files="$$(find cmd internal tests tools -type f -name '*.go' -print)"; \
	if [[ -n "$$files" ]]; then $(GOFMT) -w $$files; fi

format-check:
	@files="$$(find cmd internal tests tools -type f -name '*.go' -print)"; \
	unformatted="$$(if [[ -n "$$files" ]]; then $(GOFMT) -l $$files; fi)"; \
	if [[ -n "$$unformatted" ]]; then \
		printf '%s\n' "$$unformatted"; \
		printf 'Go files are not formatted. Run make format.\n' >&2; \
		exit 1; \
	fi

lint:
	./tools/check-repository.sh --self-test
	./tools/check-repository.sh
	$(GO) tool actionlint
	$(GO) vet ./...
	$(PNPM) exec turbo run lint

typecheck:
	$(GO) test -run '^$$' ./...
	$(PNPM) exec turbo run typecheck

test:
	@mkdir -p coverage
	$(GO) test -race -coverprofile=coverage/go.out $(GO_PACKAGES)
	$(GO) test -race ./tools/...
	$(PNPM) exec turbo run test

coverage: test
	$(GO) tool cover -func=coverage/go.out

coverage-check: test
	$(GO) run ./tools/coveragecheck \
		-profile coverage/go.out \
		-policy tools/coverage-policy.json \
		-base "$(COVERAGE_BASE_REF)"

license-check:
	$(GO) run ./tools/licensecheck check -report $(LICENSE_REPORT)

license-report: license-check

security:
	$(GO) mod verify
	$(GO) tool govulncheck ./...
	$(PNPM) audit --audit-level high

conformance:
	$(GO) test ./tests/conformance/...

integration:
	$(GO) test ./tests/integration/...

build:
	@if [[ "$(VERSION)" != "0.0.0-dev" && "$(RELEASE_MANIFEST)" == "deploy/development-release-manifest.json" ]]; then \
		printf 'A non-development VERSION requires an explicit verified RELEASE_MANIFEST.\n' >&2; \
		exit 1; \
	fi
	@mkdir -p dist
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false \
		-ldflags "-s -w -X main.releaseVersion=$(VERSION) -X main.sourceRevision=$(SOURCE_REVISION) -X main.releaseManifestDigest=$(RELEASE_MANIFEST_DIGEST)" \
		-o $(ENGINE_BINARY) ./cmd/layerdraw-engine
	cp $(RELEASE_MANIFEST) dist/layerdraw-engine.release-manifest.json
	$(PNPM) exec turbo run build

engine-wasm:
	ENGINE_WASM_OUTPUT_DIR="$(CURDIR)/$(ENGINE_WASM_DIR)" \
		VERSION="$(VERSION)" SOURCE_REVISION="$(SOURCE_REVISION)" \
		./tools/build-engine-wasm.sh

engine-wasm-check: engine-wasm
	$(GO) run ./tools/wasmartifact verify -output "$(ENGINE_WASM_DIR)"
	LAYERDRAW_ENGINE_WASM_DIR="$(CURDIR)/$(ENGINE_WASM_DIR)" \
		$(GO) test -run EngineWASM ./tests/packaged/...

engine-wasm-reproducible:
	VERSION="$(VERSION)" SOURCE_REVISION="$(SOURCE_REVISION)" \
		./tools/check-engine-wasm-reproducible.sh

protocol-package-check: build
	./tools/check-protocol-package.sh

package: build
	$(GO) run ./tools/licensecheck bundle \
		-binary $(ENGINE_BINARY) \
		-output dist \
		-version $(VERSION)

verify-packaged: package
	LAYERDRAW_ENGINE_BINARY="$(CURDIR)/$(ENGINE_BINARY)" \
		LAYERDRAW_BUNDLE_DIR="$(CURDIR)/dist" \
		$(GO) test ./tests/packaged/...

ci: generate-check format-check lint typecheck coverage-check conformance integration license-check security protocol-package-check package verify-packaged engine-wasm-check engine-wasm-reproducible

clean:
	rm -rf dist coverage reports .turbo
