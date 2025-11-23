FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o ip2country .

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/ip2country .
COPY --from=builder /app/data ./data

CMD ["./ip2country"]