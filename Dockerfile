# Stage 1: Build static Go binary
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile optimized static binary
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o meters-reader ./cmd/meters-reader

# Stage 2: Minimal runtime image
FROM alpine:3.21

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/meters-reader /app/meters-reader

# Default port for web service (Koyeb overrides with $PORT)
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/app/meters-reader", "-server"]
