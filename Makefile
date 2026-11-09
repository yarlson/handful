MISE := mise exec --
export BASE ?= origin/main

.PHONY: build ui generate test run app app-universal app-universal-deps app-universal-package icons fmt check fmt-check lint generate-check mod-check race vuln ui-check web-check desktop-check workflow-check crap mutation mutation-diff ui-mutation

# SERVER_VERSION is the version the server reports, without the tag's "v":
# VERSION when given (the release workflow passes the tag), else the tag of
# the checked-out commit, else dev-<short sha>. The server checks for new
# releases only with a release version.
SERVER_VERSION ?= $(or $(VERSION),$(patsubst v%,%,$(shell git describe --tags --exact-match 2>/dev/null)),dev-$(shell git rev-parse --short HEAD 2>/dev/null))
GO_LDFLAGS = -ldflags "-X main.version=$(SERVER_VERSION)"

# app-universal leaves a universal binary in bin/yargpt, which go build does
# not overwrite. The server needs no cgo on macOS. On other systems cgo keeps
# the system DNS resolver, for example for .local names.
build: ui
	rm -f bin/yargpt
	$(if $(filter Darwin,$(shell uname -s)),CGO_ENABLED=0) $(MISE) go build $(GO_LDFLAGS) -o bin/yargpt ./cmd/yargpt

ui:
	cd ui && $(MISE) npm ci && $(MISE) npm run build

generate:
	$(MISE) sqlc generate

test:
	$(MISE) go test ./...
	cd ui && $(MISE) npm run typecheck && $(MISE) npm test

run: build
	./bin/yargpt

# desktop/package.json signs the app ad-hoc. When CSC_LINK holds a Developer ID
# certificate, as in the release workflow, electron-builder signs with it and
# the hardened runtime instead, and notarizes the app when the APPLE_API_*
# variables are set.
MAC_SIGNING = $(if $(CSC_LINK),-c.mac.identity="Developer ID Application" -c.mac.hardenedRuntime=true)

# Builds the app for this Mac, with the server inside it:
# desktop/dist/mac-arm64/YarGPT.app, or desktop/dist/mac/YarGPT.app on an Intel Mac.
app: build
	cd desktop && $(MISE) npm ci && $(MISE) npm run build -- $(MAC_SIGNING)

# Builds desktop/dist/YarGPT-<version>-universal-mac.zip, the app for Apple
# silicon and Intel Macs. VERSION, if set, replaces the version in
# desktop/package.json.
app-universal: app-universal-deps app-universal-package

# Builds the UI and the universal server and installs the desktop packages.
# The release workflow runs it before it hands the signing secrets to
# app-universal-package, so that no package install script sees them.
app-universal-deps: ui
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(MISE) go build $(GO_LDFLAGS) -o bin/yargpt-arm64 ./cmd/yargpt
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(MISE) go build $(GO_LDFLAGS) -o bin/yargpt-amd64 ./cmd/yargpt
	lipo -create -output bin/yargpt bin/yargpt-arm64 bin/yargpt-amd64
	cd desktop && $(MISE) npm ci

app-universal-package:
	cd desktop && $(MISE) npm run build:universal -- $(MAC_SIGNING) $(if $(VERSION),-c.extraMetadata.version=$(VERSION))

# Regenerates the committed icons from their SVG sources. It needs
# rsvg-convert (Homebrew librsvg), which mise does not have, and iconutil.
icons:
	rsvg-convert -w 32 -h 32 cmd/yargpt/tray.svg -o cmd/yargpt/tray.png
	sips -s dpiWidth 144 -s dpiHeight 144 cmd/yargpt/tray.png >/dev/null
	rm -rf target/icon.iconset && mkdir -p target/icon.iconset
	rsvg-convert -w 824 -h 824 --page-width 1024 --page-height 1024 --left 100 --top 100 ui/public/logo.svg -o target/icon-1024.png
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s target/icon-1024.png --out target/icon.iconset/icon_$${s}x$${s}.png >/dev/null && \
		sips -z $$((s * 2)) $$((s * 2)) target/icon-1024.png --out target/icon.iconset/icon_$${s}x$${s}@2x.png >/dev/null || exit 1; \
	done
	iconutil -c icns target/icon.iconset -o desktop/build/icon.icns
	sips -z 180 180 ui/public/icon-maskable-512.png --out web/public/apple-touch-icon.png >/dev/null

fmt:
	$(MISE) golangci-lint fmt
	cd ui && $(MISE) npm run format

check: fmt-check lint generate-check mod-check race vuln ui-check web-check desktop-check workflow-check

fmt-check:
	$(MISE) golangci-lint fmt --diff

# CI runs on Linux, so go vet with GOOS=darwin also compiles the macOS-only
# code, such as the menu-bar icon.
lint:
	$(MISE) golangci-lint run ./...
	CGO_ENABLED=0 GOOS=darwin $(MISE) go vet ./...

generate-check:
	$(MISE) sqlc diff

mod-check:
	$(MISE) go mod tidy -diff
	$(MISE) go mod verify

race:
	$(MISE) go test -race ./...

vuln:
	$(MISE) govulncheck ./...

ui-check:
	cd ui && $(MISE) npm ci && $(MISE) npm run format:check && $(MISE) npm run lint && $(MISE) npm run typecheck && $(MISE) npm test && $(MISE) npm run build

web-check:
	cd web && $(MISE) npm ci && $(MISE) npm run build

desktop-check:
	cd desktop && $(MISE) npm ci && $(MISE) npm test

workflow-check:
	$(MISE) actionlint

# crap4go scores every .go file it finds; pass only hand-written production files.
CRAP_SOURCES = $(shell git ls-files '*.go' ':!:*_test.go' ':!:internal/oauthtest/*' | xargs grep -L '^// Code generated .* DO NOT EDIT\.$$')

crap:
	$(MISE) crap4go $(CRAP_SOURCES)

# gremlins leaves timed-out mutants out of its percentages, so they change with
# machine speed. This counts a timeout as a detected mutant instead.
MUTATION_FLOORS = [.files[].mutations[].status] \
	| (map(select(. == "KILLED" or . == "TIMED OUT")) | length) as $$detected \
	| (map(select(. == "LIVED")) | length) as $$lived \
	| (map(select(. == "NOT COVERED")) | length) as $$uncovered \
	| (100 * $$detected / ($$detected + $$lived)) as $$efficacy \
	| (100 * ($$detected + $$lived) / ($$detected + $$lived + $$uncovered)) as $$coverage \
	| ("efficacy \($$efficacy | floor)%, mutant coverage \($$coverage | floor)%, floors 81% and 85%\n" | stderr) \
	| $$efficacy >= 81 and $$coverage >= 85

# -count=1 keeps go test from serving gremlins' coverage run from its cache:
# gremlins derives each mutant's timeout from how long that run takes.
mutation:
	rm -f target/mutation-results.json && mkdir -p target
	GOFLAGS=-count=1 $(MISE) gremlins unleash --config .gremlins.yaml .
	$(MISE) jq -e '$(MUTATION_FLOORS)' target/mutation-results.json > /dev/null

# gremlins 0.6.0 maps each diff hunk to one range of added lines, so it skips
# the mutants of a second change in the same hunk. A hunk without context
# lines holds one change.
ZERO_CONTEXT_DIFF = GIT_CONFIG_COUNT=2 GIT_CONFIG_KEY_0=diff.context GIT_CONFIG_VALUE_0=0 \
	GIT_CONFIG_KEY_1=diff.interHunkContext GIT_CONFIG_VALUE_1=0

mutation-diff:
	@if git diff --quiet --merge-base "$$BASE" -- '*.go' ':!:*_test.go'; then \
		echo "no production Go changes since $$BASE"; \
	else \
		rm -f target/mutation-results.json && mkdir -p target && \
		GOFLAGS=-count=1 $(ZERO_CONTEXT_DIFF) $(MISE) gremlins unleash --config .gremlins.yaml --diff "$$BASE" . || exit 1; \
		$(MISE) jq -e '.mutants_lived == 0 and .mutants_not_covered == 0' target/mutation-results.json > /dev/null || \
			{ echo "a line changed since $$BASE has a mutant that lived or that no test runs: add a test that kills it" >&2; exit 1; }; \
	fi

ui-mutation:
	cd ui && $(MISE) npm run mutation
