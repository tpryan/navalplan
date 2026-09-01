FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -v -o nautical-sync .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/nautical-sync /nautical-sync

ENTRYPOINT ["/nautical-sync"]
