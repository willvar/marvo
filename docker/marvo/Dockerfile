FROM node:24-bookworm-slim AS web

WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend ./
RUN npm run build

FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
COPY --from=web /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 go build -trimpath -tags marvo_web -ldflags="-s -w" -o /out/marvo .

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /out/marvo /usr/local/bin/marvo

WORKDIR /app
USER 10001:10001
EXPOSE 9989
ENTRYPOINT ["/usr/local/bin/marvo"]
CMD ["-c", "/etc/marvo/config.yaml"]
