.PHONY: build build-frontend build-agent build-runtime rebuild-images start-runtime stop-runtime wait-runtime dev preview android-debug android-apk test test-go test-android test-runtime test-webkit lint lint-go lint-frontend lint-android format-android deadcode audit clean

SHELL := /bin/bash

VERSION := $(shell cat VERSION 2>/dev/null || echo "0.1.0")
CONFIG_FILE ?= config.yaml
ANDROID_CHECK_ORIGIN ?= http://127.0.0.1:5080
ANDROID_GRADLE := frontend/android/run-gradle.sh -p frontend/android

# Marvo is a standalone module. Ignore unrelated go.work files inherited from
# parent directories unless the caller explicitly selects a workspace.
GOWORK ?= off
export GOWORK

define run-local-stack
	@set -Eeuo pipefail; \
	api_pid=""; \
	ui_pid=""; \
	cleanup() { \
		trap - EXIT INT TERM; \
		for pid in "$$api_pid" "$$ui_pid"; do \
			if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then kill "$$pid" 2>/dev/null || true; fi; \
		done; \
		for pid in "$$api_pid" "$$ui_pid"; do \
			if [ -n "$$pid" ]; then wait "$$pid" 2>/dev/null || true; fi; \
		done; \
	}; \
	trap cleanup EXIT INT TERM; \
	go run . -c "$(CONFIG_FILE)" & api_pid="$$!"; \
	$(1) & ui_pid="$$!"; \
	set +e; \
	wait -n "$$api_pid" "$$ui_pid"; \
	status="$$?"; \
	set -e; \
	exit "$$status"
endef

build: build-frontend
	go test -tags marvo_web ./frontend
	CGO_ENABLED=0 go build -tags marvo_web -ldflags "-s -w -X 'main.Version=$(VERSION)'" -o dist/marvo .

build-frontend:
	npm --prefix frontend run build

android-debug:
	@set -e; \
		server_origin="$$(go run ./cmd/marvo-config -c "$(CONFIG_FILE)" public-url)"; \
		$(MAKE) --no-print-directory build-frontend; \
		frontend/android/run-gradle.sh -p frontend/android :app:assembleDebug -Pmarvo.serverOrigin="$$server_origin"
	@mkdir -p dist/android
	cp frontend/android/app/build/outputs/apk/debug/app-debug.apk dist/android/Marvo-debug.apk

android-apk:
	@test -f frontend/android/signing.properties || { echo "frontend/android/signing.properties is required for a release APK" >&2; exit 1; }
	@set -e; \
		server_origin="$$(go run ./cmd/marvo-config -c "$(CONFIG_FILE)" public-url)"; \
		$(MAKE) --no-print-directory build-frontend; \
		frontend/android/run-gradle.sh -p frontend/android :app:assembleRelease -Pmarvo.serverOrigin="$$server_origin"
	@mkdir -p dist/android
	@version_name="$$(sed -n 's/^VERSION_NAME=//p' frontend/android/version.properties)"; \
		test -n "$$version_name"; \
		cp frontend/android/app/build/outputs/apk/release/app-release.apk "dist/android/Marvo-$$version_name.apk"; \
		echo "Built dist/android/Marvo-$$version_name.apk"

build-agent:
	MARVO_FORCE_REBUILD=1 bash docker/runtime/images.sh agent

build-runtime:
	MARVO_FORCE_REBUILD=1 bash docker/runtime/images.sh runtime

rebuild-images:
	MARVO_FORCE_REBUILD=1 bash docker/runtime/images.sh all

start-runtime:
	./docker/runtime/start.sh
	@$(MAKE) --no-print-directory wait-runtime

stop-runtime:
	./docker/runtime/stop.sh

wait-runtime:
	@command -v curl >/dev/null 2>&1 || { echo "curl is required to check runtime readiness" >&2; exit 1; }; \
	runtime_port="$${MARVO_RUNTIME_PORT:-4097}"; \
	health_url="http://127.0.0.1:$${runtime_port}/health"; \
	timeout_seconds="$${MARVO_RUNTIME_READY_TIMEOUT:-60}"; \
	case "$$timeout_seconds" in ''|*[!0-9]*) echo "MARVO_RUNTIME_READY_TIMEOUT must be a positive integer" >&2; exit 1 ;; esac; \
	if [ "$$timeout_seconds" -le 0 ]; then echo "MARVO_RUNTIME_READY_TIMEOUT must be greater than zero" >&2; exit 1; fi; \
	deadline=$$(( $$(date +%s) + timeout_seconds )); \
	until curl -fsS --max-time 2 "$$health_url" >/dev/null 2>&1; do \
		if [ "$$(date +%s)" -ge "$$deadline" ]; then \
			echo "Runtime gateway did not become ready within $${timeout_seconds}s: $$health_url" >&2; \
			docker logs --tail 100 marvo-runtime >&2 2>/dev/null || true; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	echo "Runtime gateway ready: $$health_url"

dev: start-runtime
	$(call run-local-stack,npm --prefix frontend run dev)

preview: start-runtime build-frontend
	$(call run-local-stack,npm --prefix frontend run preview -- --host 0.0.0.0)

test: test-go test-android

test-go: build-frontend
	go test ./...
	go test -tags marvo_web ./frontend

test-android: build-frontend
	$(ANDROID_GRADLE) :app:testDebugUnitTest -Pmarvo.serverOrigin="$(ANDROID_CHECK_ORIGIN)"

test-runtime:
	go test ./internal/runtimegateway ./internal/runtimeauth

test-webkit:
	npm --prefix frontend run test:e2e:webkit

lint: lint-go lint-frontend lint-android

lint-go:
	@unformatted="$$(gofmt -l $$(find . -type f -name '*.go'))"; \
	if [ -n "$$unformatted" ]; then echo "$$unformatted"; exit 1; fi
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
	go mod tidy -diff

lint-frontend:
	npm --prefix frontend run check

lint-android: build-frontend
	$(ANDROID_GRADLE) :app:lintDebug :app:detekt :app:ktlintCheck -Pmarvo.serverOrigin="$(ANDROID_CHECK_ORIGIN)"

format-android:
	$(ANDROID_GRADLE) :app:ktlintFormat

deadcode:
	@unused="$$(go run golang.org/x/tools/cmd/deadcode@v0.48.0 -tags marvo_web ./...)"; \
	if [ -n "$$unused" ]; then echo "$$unused"; exit 1; fi
	npm --prefix frontend run deadcode

audit: lint deadcode test build-frontend

clean:
	rm -rf dist/
