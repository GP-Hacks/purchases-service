FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY purchases/go.mod ./
RUN go mod download

COPY . .

WORKDIR /app/purchases/cmd/purchases
RUN go build -o purchases_service

FROM alpine:latest
WORKDIR /root/

COPY --from=builder /app/purchases/cmd/purchases/purchases_service .

EXPOSE 8080

CMD ["./purchases_service"]
