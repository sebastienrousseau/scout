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

.PHONY: spec spec-verify reuse-lint reuse-lock web-shell all build docs test test-race vet lint format spdx-check example-check perf \
        fuzz sbom coverage bench api-check checks checks-verify docs-lock \
        ecosystem ecosystem-verify commitlint ssg-check site clean help

all: format vet lint spdx-check example-check ecosystem-verify ssg-check test test-race build

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

# The one budget that is a size rather than a duration.
#
# A single static binary a security team can approve in an afternoon is
# the product, not an aesthetic, and the way that stops being true is one
# dependency at a time. 18 MiB against roughly 13 today: headroom for
# growth somebody chose, and a red build for growth nobody noticed.
#
# The allocation budgets live in internal/report/perf_test.go, where they
# run with the rest of the suite. Wall-clock budgets are measured and
# published rather than gated -- a time limit on a shared runner is a
# flaky gate, and a flaky gate teaches people to re-run the build.
BINARY_BUDGET_MIB ?= 18
perf: build
	@size=$$(wc -c < $(DIST)/$(BINARY_NAME)); \
	mib=$$(( size / 1048576 )); \
	if [ "$$mib" -gt "$(BINARY_BUDGET_MIB)" ]; then \
	  echo "perf: $(BINARY_NAME) is $${mib} MiB, over the $(BINARY_BUDGET_MIB) MiB budget" >&2; \
	  echo "perf: a static binary people can approve quickly is the product; if this growth" >&2; \
	  echo "perf: was deliberate, move BINARY_BUDGET_MIB in the same commit" >&2; \
	  exit 1; \
	fi; \
	echo "perf: $(BINARY_NAME) is $${mib} MiB, within the $(BINARY_BUDGET_MIB) MiB budget"

# API-breakage check against the last release tag. gorelease reports
# removed or changed exported identifiers; a pre-1.0 module may accept them,
# but they must be seen and named in the CHANGELOG.
# Everything the Pages workflow builds, in the same order. Running `ssg
# build` alone wipes dist and leaves /manual and /sample 404 until someone
# remembers the other three steps — which is a thing to automate, not to
# remember.
# The icons are copied, not generated: ssg wipes its output directory on
# every build, so anything committed inside it is destroyed. Keeping the
# source in brand/ and copying afterwards also means neither this build nor
# CI needs ImageMagick. scripts/gen-icons.sh regenerates them when the mark
# changes.
ICONS = favicon.ico favicon.svg apple-touch-icon.png icon-192.png icon-512.png icon-maskable-512.png

site: web-shell
	ssg build -f site/ssg.toml
	@# ssg 0.0.63 fingerprints assets but its syntax-highlight plugin injects
	@# a link to the unfingerprinted name, so every page with a code block
	@# asked for /highlight.css and got a 404 — invisibly, because a missing
	@# stylesheet renders as an unstyled page rather than as an error. Until
	@# that is fixed upstream the fingerprinted file is also published under
	@# the name the page actually asks for. ssgcheck's asset invariant is what
	@# found it and is what will notice if this line stops being needed.
	@for f in site/dist/highlight.*.css; do 	  test -e "$$f" && cp "$$f" site/dist/highlight.css; 	done
	mkdir -p site/dist/images && cp -R site/images/. site/dist/images/
	for f in $(ICONS); do cp brand/$$f site/dist/$$f; done
	python3 -m mkdocs build --strict --site-dir site/dist/manual
	go build -o $(DIST)/scout-sample ./cmd/scout
	go run ./scripts/samplereport/main.go $(DIST)/scout-sample site/dist/sample
	go run ./scripts/sitemap/main.go site/dist
	@echo "site/dist is complete: /, /manual, /sample, /images"

# The shell `scout serve` embeds. It is committed because the binary must
# carry it, and it goes stale the moment anything under web/ changes without
# this being run — which is how it shipped advertising the wrong check count
# with every asset under a /scout/ base path.
web-shell:
	ssg build -f web/ssg.toml
	for f in $(ICONS); do cp brand/$$f internal/web/dist/$$f; done
	@# ssg empties its output directory before writing, so a run that dies
	@# partway leaves the embed with nothing in it — and the only thing that
	@# notices is `go build`, several steps later, complaining about a
	@# pattern that matches no files. Assert here, where the cause is.
	@test -s internal/web/dist/index.html || { \
	  echo "web-shell: internal/web/dist is empty; ssg wiped it and did not finish" >&2; exit 1; }
	@# The stamp is what lets `make ssg-check` tell a current shell from a
	@# stale one without ssg and without rebuilding. Written last, so a build
	@# that died partway does not certify itself.
	go run ./scripts/ssgcheck/main.go -stamp
	@echo "web-shell: $$(find internal/web/dist -type f | wc -l | tr -d ' ') files embedded, sources stamped"

checks:
	go run ./scripts/checkinventory/main.go

checks-verify:
	go run ./scripts/checkinventory/main.go -check

# The Apache-2.0 attestation format (ADR 0011): the predicate's JSON Schema
# and the rubric as data, generated from the Go that implements them.
spec:
	go run ./scripts/specgen/main.go

spec-verify:
	go run ./scripts/specgen/main.go -check

# REUSE compliance over the whole tree: every file's copyright and licence,
# including the ones a header cannot carry. The linter is installed from a
# hash-pinned lock, as the docs toolchain is.
reuse-lint:
	@command -v reuse >/dev/null 2>&1 || { \
	  echo "reuse is missing. Install it with: python3 -m pip install --require-hashes -r .github/reuse-requirements.txt"; exit 1; }
	reuse lint

reuse-lock:
	uv pip compile --generate-hashes --python-version 3.12 \
	  .github/reuse-requirements.in -o .github/reuse-requirements.txt

# The family manifest in internal/ecosystem is the single source; the table in
# docs/ecosystem.md and the ecosystem.json that other repositories read are
# both generated from it. REPO-STANDARD requires that table to be CI-checked
# so a multi-repository family cannot drift.
ecosystem:
	go run ./scripts/ecosystem/main.go

ecosystem-verify:
	go run ./scripts/ecosystem/main.go -check

# Every page scout publishes is generated by ssg from a theme in the SSG
# theme suite. THEME points at a checkout of that suite to also measure how
# far the vendored layouts have drifted from it; without it the offline
# checks still run, which is what CI does.
THEME ?=
ssg-check:
	go run ./scripts/ssgcheck/main.go $(if $(THEME),-theme $(THEME) -strict,)

# AGENTS.md section 4, on the commits this change adds and never on the
# history behind them. RANGE overrides the default for a local check.
RANGE ?= origin/main..HEAD
commitlint:
	go run ./scripts/commitlint.go "$(RANGE)"

docs-lock:
	@command -v pip-compile >/dev/null 2>&1 || { \
	  echo "pip-compile is missing. Install it with: python3 -m pip install pip-tools"; exit 1; }
	pip-compile --generate-hashes --strip-extras --allow-unsafe \
	  --output-file=docs/requirements.txt docs/requirements.in

# gorelease exits non-zero for two unrelated reasons: it found an
# incompatible change, or it could not suggest a version. The second happens
# for a few minutes after every tag, while the module proxy catches up —
# "Can only suggest a release version when compared against the most recent
# version of this major" — and failing the branch for it teaches people that
# a red API gate means nothing. Only an incompatible change fails here.
api-check:
	@tag=$$(git describe --tags --abbrev=0 2>/dev/null || true); \
	if [ -z "$$tag" ]; then echo "api-check: no release tag yet, nothing to compare"; exit 0; fi; \
	out=$$(go run golang.org/x/exp/cmd/gorelease@latest -base="$$tag" 2>&1); rc=$$?; \
	printf '%s\n' "$$out"; \
	if printf '%s' "$$out" | grep -qiE 'incompatible changes'; then \
	  echo "api-check: the public API changed incompatibly against $$tag"; exit 1; \
	fi; \
	if [ $$rc -ne 0 ] && ! printf '%s' "$$out" | grep -q 'Cannot suggest a release version'; then \
	  echo "api-check: gorelease failed against $$tag"; exit $$rc; \
	fi; \
	echo "api-check: no incompatible change against $$tag"

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
