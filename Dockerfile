# Build stage
FROM golang:1.21.4-alpine AS builder

# Install git (needed for go modules sometimes)
RUN apk add --no-cache git

WORKDIR /app

# Copy and download modules first (cache dependencies)
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY *.go ./

# Build statically linked binary for Linux (alpine-based)
RUN CGO_ENABLED=0 GOOS=linux go build -o /auto_scale

# Final stage: minimal image with just the binary
FROM scratch

# Copy CA certificates from the builder stage
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# Copy binary from builder stage
COPY --from=builder /auto_scale /auto_scale

# Expose port 8080
EXPOSE 8080

# Run the binary
CMD ["/auto_scale"]
