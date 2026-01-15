FROM docker.io/golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go build

FROM docker.io/alpine:latest

COPY --from=builder /src/dnsthing /usr/local/bin/dnsthing
RUN apk add dnsmasq bind-tools
RUN mkdir -p /data

CMD ["dnsthing", "--replace", "--minimum-update-interval", "1s", "/data/hosts"]
