FROM golang:1.24.2-bookworm AS builder

ENV CGO_ENABLED=1
ENV GOOS=windows
ENV GOARCH=amd64
ENV CC=x86_64-w64-mingw32-gcc

# Используем зеркало для ускорения загрузки
RUN sed -i 's/deb.debian.org/mirror.yandex.ru/g' /etc/apt/sources.list

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc-mingw-w64 \
    libx11-dev \
    xorg-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /app/dist && \
    go build -ldflags="-s -w" -o /app/dist/ClickControlApp.exe .

FROM scratch
COPY --from=builder /app/dist/ClickControlApp.exe /dist/