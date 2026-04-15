FROM rust:1.82-alpine AS builder
RUN apk add --no-cache musl-dev
WORKDIR /app
COPY Cargo.toml Cargo.lock ./
COPY src ./src
RUN cargo build --release

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/target/release/prequal-backend /usr/local/bin/
EXPOSE 8080
CMD ["prequal-backend"]
