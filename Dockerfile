# --- Stage 1: Builder ---
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build the binary
COPY . .
# CGO_ENABLED=0 ensures a static binary that runs on Alpine
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# --- Stage 2: Runtime ---
FROM alpine:latest

WORKDIR /root/

# Install ca-certificates (for Auth and PubSub) AND mailcap (for MIME types)
RUN apk --no-cache add ca-certificates mailcap

# Copy binary and templates from builder
COPY --from=builder /app/main .
COPY --from=builder /app/views ./views
COPY --from=builder /app/public ./public

# Cloud Run injects the PORT env var
ENV PORT=8080

CMD ["./main"]