# Build Stage
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Copy go mod and sum files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
# Use flags to reduce binary size
RUN go build -ldflags="-w -s" -o nas-api main.go

# Run Stage
FROM alpine:latest

WORKDIR /app

# Install runtime dependencies
# sudo is used in main.go for system operations
# bash and iproute2 are useful for networking/system tools
RUN apk add --no-cache sudo bash iproute2 ca-certificates

# Create a non-root user for security
RUN adduser -D nasuser && \
    echo "nasuser ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/nasuser && \
    chmod 0440 /etc/sudoers.d/nasuser

# Copy the binary from builder
COPY --from=builder /app/nas-api .
# Copy static assets if they are not embedded
COPY --from=builder /app/public ./public
# Copy setup scripts (needed for sudoers check in main.go)
COPY setup-nas-sudo.sh setup-nas-sudo.py ./

# Create data directory for SQLite database
RUN mkdir -p /app/data && chown -R nasuser:nasuser /app/data

# Ensure binary is executable
RUN chmod +x nas-api

USER nasuser

# Default port for the Fiber app
EXPOSE 3000

# Environment variables can be overridden at runtime
ENV SAMBA_HOME_BASE=/srv/samba/homes
ENV JWT_SECRET=change_me_in_production

CMD ["./nas-api"]
