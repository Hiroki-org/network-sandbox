FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o worker-go .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/worker-go .

ENV PORT=8080
ENV WORKER_NAME=go-worker-1
ENV WORKER_COLOR=#3B82F6
ENV MAX_CONCURRENT_REQUESTS=10
ENV RESPONSE_DELAY_MS=100
ENV FAILURE_RATE=0.0
ENV QUEUE_SIZE=50

EXPOSE 8080

CMD ["./worker-go"]
