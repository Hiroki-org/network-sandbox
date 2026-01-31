FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod ./
COPY *.go ./

RUN go mod tidy && go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o load-balancer .

FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/load-balancer .

ENV PORT=8080
ENV LB_ALGORITHM=round-robin
ENV WORKER_GO_URL=http://worker-go:8080
ENV WORKER_RUST_URL=http://worker-rust:8080
ENV WORKER_PYTHON_URL=http://worker-python:8080

EXPOSE 8080

CMD ["./load-balancer"]
