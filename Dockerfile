ARG GO_VERSION=1.26.1

FROM golang:${GO_VERSION}-bookworm AS deps
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

FROM golang:${GO_VERSION}-bookworm AS builder
WORKDIR /src
COPY --from=deps /go/pkg /go/pkg
COPY . .
RUN PWGO_VER="$(go list -m -f '{{.Version}}' github.com/mxschmitt/playwright-go)" \
    && go install github.com/mxschmitt/playwright-go/cmd/playwright@${PWGO_VER}
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/kleinanzeigen-finder .

FROM ubuntu:noble AS runtime

ENV TZ=Europe/Berlin \
    PLAYWRIGHT_BROWSERS_PATH=/ms-playwright

WORKDIR /app

COPY --from=builder /go/bin/playwright /usr/local/bin/playwright

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && playwright install --with-deps chromium \
    && mkdir -p /app/data /app/logs \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/kleinanzeigen-finder /usr/local/bin/kleinanzeigen-finder

VOLUME ["/app/data", "/app/logs"]

ENTRYPOINT ["/usr/local/bin/kleinanzeigen-finder"]
CMD ["run"]
