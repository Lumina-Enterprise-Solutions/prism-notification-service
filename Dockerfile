# Tahap 1: Builder - Kompilasi kode Go
FROM golang:1.24-alpine AS builder
ENV CGO_ENABLED=0
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Membangun package main di direktori saat ini
RUN go build -ldflags="-w -s" -o /app/server .

# Tahap 2: Final - Image runtime yang ramping dan aman
FROM alpine:latest
WORKDIR /app
# Menambahkan user non-root untuk keamanan
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
# Salin binary dari tahap builder
COPY --from=builder /app/server .
# PENTING: Salin direktori templates agar bisa diakses oleh aplikasi
COPY --from=builder /app/templates ./templates
# Label standar untuk metadata image
LABEL org.opencontainers.image.source="https://github.com/Lumina-Enterprise-Solutions/prism-notification-service"
# Atur kepemilikan dan ganti user
RUN chown -R appuser:appgroup /app
USER appuser
# Expose port (sebagai dokumentasi)
EXPOSE 8080
# Perintah default untuk menjalankan aplikasi
CMD ["./server"]
