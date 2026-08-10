.PHONY: build build-frontend start-opencode wait-opencode dev preview test test-webkit lint lint-go lint-frontend deadcode audit clean

VERSION := $(shell cat VERSION 2>/dev/null || echo "0.1.0")

build:
	CGO_ENABLED=0 go build -ldflags "-X 'main.Version=$(VERSION)'" -o dist/marvo .

build-frontend:
	npm --prefix frontend run build

start-opencode:
	./docker/opencode/start.sh
	@$(MAKE) --no-print-directory wait-opencode

wait-opencode:
	@command -v curl >/dev/null 2>&1 || { echo "curl is required to check OpenCode readiness" >&2; exit 1; }; \
	opencode_port="$${MARVO_OPENCODE_PORT:-4096}"; \
	health_url="$${MARVO_OPENCODE_HEALTH_URL:-http://127.0.0.1:$${opencode_port}/global/health}"; \
	timeout_seconds="$${MARVO_OPENCODE_READY_TIMEOUT:-60}"; \
	case "$$timeout_seconds" in ''|*[!0-9]*) echo "MARVO_OPENCODE_READY_TIMEOUT must be a positive integer" >&2; exit 1 ;; esac; \
	if [ "$$timeout_seconds" -le 0 ]; then echo "MARVO_OPENCODE_READY_TIMEOUT must be greater than zero" >&2; exit 1; fi; \
	deadline=$$(( $$(date +%s) + timeout_seconds )); \
	until curl -fsS --max-time 2 "$$health_url" >/dev/null 2>&1; do \
		if [ "$$(date +%s)" -ge "$$deadline" ]; then \
			echo "OpenCode did not become ready within $${timeout_seconds}s: $$health_url" >&2; \
			docker logs --tail 100 marvo-opencode >&2 2>/dev/null || true; \
			exit 1; \
		fi; \
		sleep 1; \
	done; \
	echo "OpenCode ready: $$health_url"

dev: start-opencode
	go run . -c config.yaml & \
	npm --prefix frontend run dev & \
	wait

preview: start-opencode build-frontend
	go run . -c config.yaml & \
	npm --prefix frontend run preview -- --host 0.0.0.0 & \
	wait

test:
	go test ./...

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
