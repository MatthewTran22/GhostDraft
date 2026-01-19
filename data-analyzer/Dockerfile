# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Run unit tests (exclude integration tests that require API keys)
# If tests fail, build fails and container won't be created
RUN go test ./... -run "^Test[^_]*$" -v

# Build all binaries
RUN go build -o /bin/ui ./cmd/ui
RUN go build -o /bin/collector ./cmd/collector
RUN go build -o /bin/reducer ./cmd/reducer

# Runtime stage
FROM golang:1.24-alpine

WORKDIR /app

# Install gh CLI for GitHub releases
RUN apk add --no-cache github-cli

# Copy built binaries
COPY --from=builder /bin/ui /app/ui
COPY --from=builder /bin/collector /app/collector
COPY --from=builder /bin/reducer /app/reducer

# Copy source (needed for 'go run' in pipeline)
COPY --from=builder /build /app

# Create data directories
RUN mkdir -p /app/data/hot /app/data/warm /app/data/cold /app/export

# Default environment
ENV PORT=8080
ENV BLOB_STORAGE_PATH=/app/data

EXPOSE 8080

# Run the UI server
CMD ["/app/ui"]
