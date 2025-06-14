# 💎 Prism Notification Service

[![Go CI Pipeline](https://github.com/Lumina-Enterprise-Solutions/prism-notification-service/actions/workflows/ci.yml/badge.svg)](https://github.com/Lumina-Enterprise-Solutions/prism-notification-service/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/Lumina-Enterprise-Solutions/prism-notification-service?style=flat-square&logo=github)](https://github.com/Lumina-Enterprise-Solutions/prism-notification-service/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/Lumina-Enterprise-Solutions/prism-notification-service)](https://goreportcard.com/report/github.com/Lumina-Enterprise-Solutions/prism-notification-service)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](./LICENSE)

Pusat notifikasi terpusat untuk ekosistem **Prism ERP**. Layanan ini bertanggung jawab untuk mengirimkan semua komunikasi keluar (seperti email, dan di masa depan, SMS atau push notification) secara andal dan terukur.

---

## ✨ Fitur Utama

-   **📨 Pemrosesan Asinkron**: Menggunakan Redis sebagai *message queue* untuk menerima permintaan notifikasi secara cepat tanpa memblokir layanan pengirim. Ini memastikan *throughput* tinggi bahkan saat beban puncak.
-   **🛡️ Andal & Tangguh**: Jika layanan pengiriman email (SMTP) sedang tidak aktif, *job* akan tetap aman di dalam antrian dan akan diproses kembali oleh *worker* secara otomatis.
-   **🔭 Dapat Diamati (Observable)**: Terintegrasi penuh dengan **OpenTelemetry (Jaeger)** untuk *distributed tracing* dan **Prometheus** untuk *metrics*, memungkinkan pemantauan dan *debugging* yang mendalam.
-   **🔒 Manajemen Rahasia Terpusat**: Mengambil kredensial sensitif (misalnya, password SMTP) secara aman dari **HashiCorp Vault**, bukan menyimpannya dalam kode atau variabel lingkungan.
-   **📦 Ringan & Efisien**: Dikemas dalam kontainer Docker menggunakan *multi-stage build*, menghasilkan *image* akhir yang kecil dan aman berbasis Alpine Linux.

---

## 🏗️ Arsitektur & Alur Kerja

Layanan ini dirancang untuk memisahkan penerimaan permintaan dari proses pengiriman, meningkatkan skalabilitas dan ketahanan sistem.

```
+----------------+      +-----------------------+      +--------------------+      +-----------------------+      +----------------+
| Service Lain   |----->|  API Endpoint         |----->|  Queue Service     |----->|  Redis Queue          |----->| Background     |
| (e.g., UserSvc)| HTTP |  (POST /notifications)|      |  (Enqueue Job)     |      |  (notification_queue) |      | Worker         |
+----------------+      +-----------------------+      +--------------------+      +-----------------------+      +-------+--------+
                                                                                                                          |
                                                                                                                          | (Dequeue Job)
                                                                                                                          |
                                                                                                               +----------v----------+
                                                                                                               |    Email Service    |-----> SMTP Server
                                                                                                               |  (Send via Mailtrap)|      (e.g., Mailtrap)
                                                                                                               +---------------------+
```

---

## 🔌 API Endpoints

### Kirim Notifikasi

Menerima permintaan untuk mengirim notifikasi dan menambahkannya ke antrian pemrosesan.

-   **Endpoint**: `POST /notifications/send`
-   **Deskripsi**: Menambahkan notifikasi ke antrian untuk pengiriman asinkron.
-   **Request Body**:
    ```json
    {
      "recipient": "user.email@example.com",
      "subject": "Reset Password Anda",
      "message": "<h1>Permintaan Reset Password</h1><p>Klik link berikut untuk mereset password Anda...</p>"
    }
    ```
-   **Responses**:
    -   `202 Accepted`: Permintaan berhasil diterima dan dimasukkan ke dalam antrian.
        ```json
        {
          "message": "Notification accepted for processing"
        }
        ```
    -   `400 Bad Request`: Format request tidak valid (misalnya email salah atau field kosong).
    -   `500 Internal Server Error`: Gagal menambahkan job ke antrian (misalnya Redis tidak tersedia).

---

## ⚙️ Konfigurasi

Layanan ini dikonfigurasi melalui *environment variables*. Kredensial sensitif diambil secara otomatis dari Vault.

| Variabel Lingkungan | Deskripsi                                                     | Default                | Diambil dari Vault? |
| ------------------- | ------------------------------------------------------------- | ---------------------- | ------------------- |
| `PORT`              | Port yang digunakan oleh server HTTP.                         | `8080`                 | Tidak               |
| `REDIS_ADDR`        | Alamat host dan port untuk Redis.                             | `cache-redis:6379`     | Tidak               |
| `JAEGER_ENDPOINT`   | Alamat OTLP gRPC collector untuk Jaeger.                      | `jaeger:4317`          | Tidak               |
| `VAULT_ADDR`        | Alamat server HashiCorp Vault.                                | `http://vault:8200`    | Tidak               |
| `VAULT_TOKEN`       | Token untuk otentikasi dengan Vault.                          | `root-token-for-dev`   | Tidak               |
| `MAILTRAP_HOST`     | Host server SMTP.                                             | -                      | **Ya**              |
| `MAILTRAP_PORT`     | Port server SMTP.                                             | -                      | **Ya**              |
| `MAILTRAP_USER`     | Username untuk otentikasi SMTP.                               | -                      | **Ya**              |
| `MAILTRAP_PASS`     | Password untuk otentikasi SMTP.                               | -                      | **Ya**              |

> **Catatan**: Variabel yang ditandai "Ya" dibaca dari Vault path `secret/data/prism` oleh aplikasi saat startup.

---

## 🚀 Cara Menjalankan & Build

### Menjalankan Secara Lokal (Docker Compose)

Layanan ini paling baik dijalankan sebagai bagian dari `docker-compose.yml` di root proyek Prism ERP, yang juga menyertakan Redis, Jaeger, dan Vault.

```yaml
# Contoh snippet di docker-compose.yml
services:
  notification-service:
    build:
      context: .
      dockerfile: services/prism-notification-service/Dockerfile
    ports:
      - "8085:8080" # Map ke port yang berbeda di host
    environment:
      - REDIS_ADDR=cache-redis:6379
      - JAEGER_ENDPOINT=jaeger:4317
      - VAULT_ADDR=http://vault:8200
      - VAULT_TOKEN=root-token-for-dev
    depends_on:
      - cache-redis
      - jaeger
      - vault
```

### Membangun Image Docker

```bash
docker build -t lumina-enterprise-solutions/prism-notification-service:latest .
```

---

## 🤝 Kontribusi

Kami menyambut kontribusi! Silakan lihat [Panduan Kontribusi Organisasi](https://github.com/Lumina-Enterprise-Solutions/.github/blob/main/CONTRIBUTING.md) untuk detail tentang proses Pull Request, konvensi *commit*, dan standar pengkodean.
