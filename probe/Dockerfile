# Build stage
FROM golang:alpine AS builder

WORKDIR /app

# Copy go mod files from parent directory
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY probe/probe.go ./probe/

# Build the sidecar binary
RUN CGO_ENABLED=0 GOOS=linux go build -o prequal-sidecar ./probe/

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/prequal-sidecar .

# Expose probe port
EXPOSE 9999

ENTRYPOINT ["./prequal-sidecar"]
