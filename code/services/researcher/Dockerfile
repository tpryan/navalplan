# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install dependencies
# We generate the vendor directory inside the container to ensure
# it matches the builder's Go version exactly.
COPY go.mod go.sum ./
RUN go mod vendor

# Copy the rest of the source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -o researcher .

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates

WORKDIR /root/
COPY --from=builder /app/researcher .
COPY --from=builder /app/prompts ./prompts

EXPOSE 8081

# Ensure the binary is executable
RUN chmod +x ./researcher

CMD ["./researcher"]
