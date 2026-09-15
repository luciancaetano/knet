# Builds the stress-echo server used by the Docker stress/e2e tests
# (tests/stress, tests/docker-e2e.sh). See docs/testing.md.

FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY . .
RUN CGO_ENABLED=0 go build -mod=vendor -o /out/stress-echo ./examples/stress-echo

FROM alpine:3.20
RUN apk add --no-cache ca-certificates busybox-extras
COPY --from=builder /out/stress-echo /usr/local/bin/stress-echo
ENV ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=2s --timeout=2s --start-period=5s --retries=10 \
    CMD nc -z 127.0.0.1 8080 || exit 1
ENTRYPOINT ["/usr/local/bin/stress-echo"]
