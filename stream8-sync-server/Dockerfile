# Stream8 Sync Server — Dockerfile (SQLite Pure-Go CGO_ENABLED=0)
FROM golang:1.22-alpine AS build
WORKDIR /src

# Download delle dipendenze Go (incluso modernc.org/sqlite)
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY web ./web

# Compilazione statica senza CGO
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/stream8-sync main.go

FROM alpine:3.19
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 stream8
WORKDIR /app
COPY --from=build /out/stream8-sync ./stream8-sync

# Dati persistenti SQLite (stream8.db + WAL/SHM file all'interno della cartella /data)
ENV DATA_DIR=/data
RUN mkdir -p /data && chown stream8:stream8 /data
VOLUME ["/data"]

ENV WEB_PORT=8080
ENV API_PORT=8081
EXPOSE 8080
EXPOSE 8081

USER stream8
ENTRYPOINT ["./stream8-sync"]
