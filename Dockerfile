# Stage 1: Build
FROM golang:alpine AS builder

WORKDIR /app

RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/vigilix-collector ./cmd/vigilix-collector/main.go

# Stage 2: Runner
FROM alpine:3.21 AS runner

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/bin/vigilix-collector /app/vigilix-collector

ENV VIGILIX_SERVER="http://nginx/api"
ENV VIGILIX_COLLECTOR_TOKEN=""

ENTRYPOINT ["/app/vigilix-collector"]
