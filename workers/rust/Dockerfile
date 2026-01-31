FROM rust:1.75-alpine AS builder

RUN apk add --no-cache musl-dev

WORKDIR /app

COPY Cargo.toml ./
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release
RUN rm -rf src

COPY src ./src
RUN touch src/main.rs
RUN cargo build --release

FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /app/target/release/worker-rust .

ENV PORT=8080
ENV WORKER_NAME=rust-worker-1
ENV WORKER_COLOR=#F97316
ENV MAX_CONCURRENT_REQUESTS=10
ENV RESPONSE_DELAY_MS=100
ENV FAILURE_RATE=0.0
ENV QUEUE_SIZE=50

EXPOSE 8080

CMD ["./worker-rust"]
