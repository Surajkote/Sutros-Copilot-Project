# Stage 1: Build
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o sovereign-backend .

# Stage 2: Serve
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/sovereign-backend .
COPY .env.example .env
EXPOSE 8080
CMD ["./sovereign-backend"]