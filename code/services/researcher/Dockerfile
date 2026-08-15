# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o researcher .

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /root/
COPY --from=builder /app/researcher .

EXPOSE 8081

# Ensure the binary is executable
RUN chmod +x ./researcher

CMD ["./researcher"]
