FROM golang:1.21.6-alpine AS builder
ENV CGO_ENABLED=1

WORKDIR /src
COPY go.mod .
COPY go.sum .

RUN go mod verify && \
    go mod download

COPY . .

RUN CGO_ENABLED=0 go build -o bin/ping cmd/ping/main.go
RUN CGO_ENABLED=0 go build -o bin/pong cmd/pong/main.go

FROM alpine:3.19
COPY --from=builder /src/bin/ping /usr/bin/ping
COPY --from=builder /src/bin/pong /usr/bin/pong