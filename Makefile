# Makefile for the prism-notification-service

# ====================================================================================
# Variabel Konfigurasi
# ====================================================================================

# Nama binary yang akan dihasilkan
BINARY_NAME := prism-notification-service
# Nama service, digunakan untuk path dan nama image
SERVICE_NAME := prism-notification-service
# Nama image Docker yang akan dibuat
DOCKER_IMAGE_NAME := lumina-enterprise-solutions/$(SERVICE_NAME)
# Tag default untuk image Docker
DOCKER_TAG := latest


# ====================================================================================
# Definisi Target
# ====================================================================================

# .PHONY memastikan bahwa target ini akan selalu dijalankan, bahkan jika ada file dengan nama yang sama.
.PHONY: all build run test lint cover clean docker-build help

# Target default yang akan dijalankan jika Anda hanya mengetik 'make'
all: build ## 🎯 Build binary aplikasi (default)

help: ## ✨ Tampilkan bantuan ini
	@echo "Perintah yang tersedia untuk $(SERVICE_NAME):"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## 🏗️  Compile aplikasi Go dan menghasilkan binary
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) .
	@echo "Build complete: ./$(BINARY_NAME)"

run: build ## 🚀 Jalankan aplikasi secara lokal (memerlukan build terlebih dahulu)
	@echo "Starting $(SERVICE_NAME)..."
	@echo "NOTE: Pastikan Vault, Redis, dan Jaeger sedang berjalan."
	# Sesuaikan variabel lingkungan ini jika setup lokal Anda berbeda
	VAULT_ADDR=http://localhost:8200 \
	VAULT_TOKEN=root-token-for-dev \
	REDIS_ADDR=localhost:6379 \
	JAEGER_ENDPOINT=localhost:4317 \
	./$(BINARY_NAME)

test: ## 🧪 Jalankan semua unit test untuk service ini
	@echo "Running tests for $(SERVICE_NAME)..."
	@go test -v -race -cover ./... # <-- PERUBAHAN: Gunakan ./...

lint: ## 🧹 Jalankan linter golangci-lint
	@echo "Running linter..."
	@golangci-lint run ./...

cover: ## 📊 Gabungkan laporan coverage dan buka di browser
	@echo "Generating combined coverage report..."
	@echo "mode: atomic" > coverage.out
	@go test -race -coverprofile=profile.out -covermode=atomic github.com/Lumina-Enterprise-Solutions/prism-notification-service/config
	@go test -race -coverprofile=profile.out -covermode=atomic github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service
	@go test -race -coverprofile=profile.out -covermode=atomic github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/websocket
	@go test -race -coverprofile=profile.out -covermode=atomic github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/handler
	@grep -h -v "^mode:" profile*.out >> coverage.out || true
	@rm profile*.out
	@echo "Opening combined coverage report..."
	@go tool cover -html=coverage.out

clean: ## 🗑️  Hapus binary yang telah di-build dan file coverage
	@echo "Cleaning up artifacts..."
	@rm -f $(BINARY_NAME)
	@rm -f coverage.out

docker-build: ## 🐳 Bangun image Docker untuk service ini
	@echo "Building Docker image: $(DOCKER_IMAGE_NAME):$(DOCKER_TAG)..."
	# Konteks build (../../) adalah root dari monorepo, ini penting
	# agar semua file (common-libs, go.work, dll) dapat disalin.
	# -f Dockerfile menunjuk ke Dockerfile di direktori ini.
	@docker build -t $(DOCKER_IMAGE_NAME):$(DOCKER_TAG) -f Dockerfile ../..
	@echo "Docker image built successfully."
