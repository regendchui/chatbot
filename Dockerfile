# Build stage: compile the bot binary (match go.mod toolchain).
FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod ./
COPY . .

RUN go mod tidy
RUN go build -o bot ./main

# Runtime: slim image with CA certs for HTTPS (Gemini, WhatsApp).
FROM debian:bookworm-slim

WORKDIR /app

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/bot /app/bot
COPY --from=builder /app/survey-config.json /app/survey-config.json

CMD ["/app/bot"]
