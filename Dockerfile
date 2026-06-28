ARG GO_VERSION=1.23
ARG ALPINE_VERSION=3.21

# ---------- builder ----------
FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w" -o /out/server ./cmd/server

# ---------- runtime ----------
FROM alpine:${ALPINE_VERSION} AS runtime

RUN apk add --no-cache ca-certificates tzdata wget \
 && addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=builder /out/server /usr/local/bin/server

USER app

EXPOSE 8080
ENV PORT=8080 \
    GOMEMLIMIT=512MiB

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/server"]
