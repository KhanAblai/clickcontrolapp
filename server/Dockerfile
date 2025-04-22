FROM golang:1.24.2-bookworm AS builder

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOINSECURE="proxy.golang.org"
ENV GOSUMDB=off

WORKDIR /app
COPY . .
RUN go mod tidy && go build -o server .

# Используем официальный образ Debian вместо gcr.io
FROM debian:bookworm-slim
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8765
CMD ["./server"]