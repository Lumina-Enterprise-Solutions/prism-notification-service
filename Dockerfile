# services/prism-user-service/Dockerfile

# --- Tahap 1: Builder ---
# Tahap ini fokus untuk meng-compile aplikasi Go kita.
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Salin file manajemen dependensi
COPY go.work go.work.sum ./
COPY common/prism-common-libs/go.mod ./common/prism-common-libs/
COPY services/prism-auth-service/go.mod ./services/prism-auth-service/
COPY services/prism-user-service/go.mod ./services/prism-user-service/
COPY services/prism-notification-service/go.mod ./services/prism-notification-service/
COPY services/prism-file-service/go.mod ./services/prism-file-service/

# Download dependensi agar bisa di-cache
RUN go mod download

# Salin semua source code
COPY . .

# Build aplikasi spesifik untuk service ini
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/main ./services/prism-notification-service


# --- Tahap 2: Final Image ---
# Tahap ini membuat image akhir yang ramping yang akan kita jalankan.
FROM alpine:latest

WORKDIR /app

# Salin binary aplikasi yang sudah di-build dari tahap 'builder'
COPY --from=builder /app/main .

# Definisikan service name untuk logging dan monitoring
ENV SERVICE_NAME=prism-notification-service

# Jalankan aplikasi secara langsung. Tidak ada lagi entrypoint script.
CMD ["./main"]
