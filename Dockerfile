FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

WORKDIR /app/cmd/purchases
RUN go build -o purchases_service

FROM alpine:latest
WORKDIR /root/

COPY --from=builder /app/cmd/purchases/purchases_service .

EXPOSE 8080

CMD ["./purchases_service"]
