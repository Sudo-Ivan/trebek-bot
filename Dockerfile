FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod ./

COPY cmd/trebek/ ./cmd/trebek/
COPY internal/ ./internal/

RUN go build -o trebek ./cmd/trebek/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/trebek .

COPY config.txt .
COPY internal/question/all.json internal/question/all.json

ENTRYPOINT ["./trebek"]
