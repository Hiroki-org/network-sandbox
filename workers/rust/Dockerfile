FROM rust:1.82-slim-bookworm AS builder

RUN apt-get update && apt-get install -y pkg-config libssl-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY Cargo.toml ./
RUN mkdir src && echo "fn main() {}" > src/main.rs
RUN cargo build --release
RUN rm -rf src

COPY src ./src
RUN touch src/main.rs
RUN cargo build --release

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates openssl && rm -rf /var/lib/apt/lists/*

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
