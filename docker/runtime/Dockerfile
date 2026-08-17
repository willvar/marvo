FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/marvo-runtime ./cmd/marvo-runtime
COPY internal/agentcredentials ./internal/agentcredentials
COPY internal/runtimeauth ./internal/runtimeauth
COPY internal/runtimeevents ./internal/runtimeevents
COPY internal/runtimegateway ./internal/runtimegateway
COPY internal/scheduling ./internal/scheduling
COPY internal/store ./internal/store
COPY internal/userid ./internal/userid
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/marvo-runtime ./cmd/marvo-runtime

FROM debian:bookworm-slim
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /out/marvo-runtime /usr/local/bin/marvo-runtime
COPY --chmod=0555 docker/runtime/healthcheck.sh /usr/local/bin/marvo-runtime-healthcheck
EXPOSE 4097
ENTRYPOINT ["/usr/local/bin/marvo-runtime"]
