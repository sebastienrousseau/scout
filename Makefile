# SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
# SPDX-License-Identifier: GPL-3.0-only
#
# Developer tasks. The Unix install contract (install/uninstall honouring
# PREFIX and DESTDIR) lives in GNUmakefile, which includes this file.

BINARY_NAME = scout
DIST ?= build
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
VERSION_PKG = github.com/sebastienrousseau/scout
LDFLAGS = -s -w -X $(VERSION_PKG)/cmd.Version=$(VERSION)
export CGO_ENABLED = 0
COVER_MIN ?= 85

.PHONY: all build docs test test-race vet lint format spdx-check example-check \
        fuzz sbom coverage bench api-check checks checks-verify docs-lock \
        site clean help

all: format vet lint spdx-check example-check test test-race build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(BINARY_NAME) ./cmd/scout

docs:
	go run ./scripts/gen_docs.go $(DIST)

test:
	go test ./...

test-race:
	go test -race -shuffle=on -count=1 ./...

coverage:
	@go test -coverprofile=$(DIST)/coverage.out ./... >/dev/null
	@go tool cover -func=$(DIST)/coverage.out | tail -1
	@go test -cover ./... 2>/dev/null | awk -v min=$(COVER_MIN) '/coverage:/ { gsub("%","",$$5); if ($$5+0 < min) { bad=1; print "below " min "%: " $$0 } } END { exit bad }'

# Benchmarks are smoke-run (one iteration) so they keep compiling; nothing
# asserts on their numbers.
bench:
	go test -run '^$$' -bench . -benchtime 1x ./...

# API-breakage check against the last release tag. gorelease reports
# removed or changed exported identifiers; a pre-1.0 module may accept them,
# but they must be seen and named in the CHANGELOG.
# Everything the Pages workflow builds, in the same order. Running `ssg
# build` alone wipes dist and leaves /manual and /sample 404 until someone
# remembers the other three steps — which is a thing to automate, not to
# remember.
site:
	ssg build -f site/ssg.toml
	mkdir -p site/dist/images && cp -R site/images/. site/dist/images/
	python3 -m mkdocs build --strict --site-dir site/dist/manual
	go build -o $(DIST)/scout-sample ./cmd/scout
	go run ./scripts/samplereport/main.go $(DIST)/scout-sample site/dist/sample
	@echo "site/dist is complete: /, /manual, /sample, /images"

checks:
	go run ./scripts/checkinventory/main.go

checks-verify:
	go run ./scripts/checkinventory/main.go -check

docs-lock:
	@command -v pip-compile >/dev/null 2>&1 || { \
	  echo "pip-compile is missing. Install it with: python3 -m pip install pip-tools"; exit 1; }
	pip-compile --generate-hashes --strip-extras --allow-unsafe \
	  --output-file=docs/requirements.txt docs/requirements.in

api-check:
	@tag=$$(git describe --tags --abbrev=0 2>/dev/null || true); \
	if [ -z "$$tag" ]; then echo "api-check: no release tag yet, nothing to compare"; exit 0; fi; \
	go run golang.org/x/exp/cmd/gorelease@latest -base="$$tag"

vet:
	go vet ./...

lint:
	golangci-lint run ./...

format:
	gofmt -l -w .

spdx-check:
	go run ./scripts/spdx_sweep.go

example-check:
	go run ./scripts/example_check.go

fuzz:
	scripts/fuzz.sh

sbom: build
	syft scan dir:. -o cyclonedx-json > $(DIST)/sbom.cdx.json

clean:
	rm -rf $(DIST)

help:
	@printf '%s\n' "targets: all build docs install uninstall install-smoke test test-race coverage bench api-check vet lint format spdx-check example-check fuzz sbom clean"
