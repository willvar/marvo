.PHONY: build build-frontend build-agent build-runtime rebuild-images start-runtime stop-runtime wait-runtime dev preview test test-runtime test-webkit lint lint-go lint-frontend deadcode audit clean

VERSION := $(shell cat VERSION 2>/dev/null || echo "0.1.0")

build: build-frontend
	CGO_ENABLED=0 go build -tags marvo_web -ldflags "-s -w -X 'main.Version=$(VERSION)'" -o dist/marvo .

build-frontend:
	npm --prefix frontend run build

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
	go run . -c config.yaml & \
	npm --prefix frontend run dev & \
	wait

preview: start-runtime build-frontend
	go run . -c config.yaml & \
	npm --prefix frontend run preview -- --host 0.0.0.0 & \
	wait

test:
	go test ./...

test-runtime:
	go test ./internal/runtimegateway ./internal/runtimeauth

test-webkit:
	npm --prefix frontend run test:e2e:webkit

lint: lint-go lint-frontend

lint-go:
	@unformatted="$$(gofmt -l $$(find . -type f -name '*.go'))"; \
	if [ -n "$$unformatted" ]; then echo "$$unformatted"; exit 1; fi
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
	go mod tidy -diff

lint-frontend:
	npm --prefix frontend run check

deadcode:
	@unused="$$(go run golang.org/x/tools/cmd/deadcode@v0.48.0 ./...)"; \
	if [ -n "$$unused" ]; then echo "$$unused"; exit 1; fi
	npm --prefix frontend run deadcode

audit: lint deadcode test build-frontend

clean:
	rm -rf dist/
