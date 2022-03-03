### Builder
FROM golang:1.17-bullseye as builder

WORKDIR /app

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

RUN go build -o knurse cmd/webhook/main.go && \
    go build -o setup-ca-certs cmd/setup-ca-certs/main.go

### Setup-ca-certs app image with certs and tz
FROM debian:bullseye-slim as setup-ca-certs
RUN apt update && \
    apt install -y ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/* \
ENV TZ Asia/Seoul
COPY --from=builder app/setup-ca-certs /
ENTRYPOINT ["/setup-ca-certs"]

### Webhook app
FROM scratch as webhook
ENV TZ Asia/Seoul
COPY --from=setup-ca-certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=setup-ca-certs /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder app/knurse /
ENTRYPOINT ["/knurse"]
