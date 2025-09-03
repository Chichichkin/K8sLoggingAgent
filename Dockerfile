# --- build stage ---
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o agent ./cmd/agent

# --- run stage ---
FROM alpine:3.19

WORKDIR /root/

RUN apk --no-cache add ca-certificates

COPY --from=builder /app/agent .

# Create non-root user
RUN addgroup -g 1000 agent && \
    adduser -u 1000 -G agent -D agent && \
    chown agent:agent /root/agent

USER agent

ENTRYPOINT ["./agent"]