// file: services/prism-notification-service/main.go
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/ginutil"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	"github.com/gin-gonic/gin"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Fungsi helper untuk mengambil rahasia dari Vault dan set sebagai env var
func loadSecretsFromVault() {
	vaultClient, err := client.NewVaultClient()
	if err != nil { // <-- PERBAIKAN: Mengatasi SA9003 (empty branch)
		log.Fatalf("Gagal membuat klien Vault: %v", err)
	}
	secretPath := "secret/data/prism"

	host, _ := vaultClient.ReadSecret(secretPath, "mailtrap_host")
	port, _ := vaultClient.ReadSecret(secretPath, "mailtrap_port")
	user, _ := vaultClient.ReadSecret(secretPath, "mailtrap_user")
	pass, _ := vaultClient.ReadSecret(secretPath, "mailtrap_pass")

	// <-- PERBAIKAN: Mengatasi 3 error dari `errcheck` dengan memeriksa return value.
	if err := os.Setenv("MAILTRAP_HOST", host); err != nil {
		log.Fatalf("Gagal mengatur env var MAILTRAP_HOST: %v", err)
	}
	if err := os.Setenv("MAILTRAP_PORT", port); err != nil {
		log.Fatalf("Gagal mengatur env var MAILTRAP_PORT: %v", err)
	}
	if err := os.Setenv("MAILTRAP_USER", user); err != nil {
		log.Fatalf("Gagal mengatur env var MAILTRAP_USER: %v", err)
	}
	if err := os.Setenv("MAILTRAP_PASS", pass); err != nil {
		log.Fatalf("Gagal mengatur env var MAILTRAP_PASS: %v", err)
	}
	log.Println("Berhasil memuat kredensial Mailtrap dari Vault.")
}

func main() {
	loadSecretsFromVault()

	emailService := service.NewEmailService()
	queueService := service.NewQueueService()

	go func() {
		log.Println("Memulai background worker untuk antrian notifikasi...")
		for {
			job, err := queueService.Dequeue(context.Background())
			if err != nil {
				log.Printf("ERROR: Gagal mengambil job dari antrian: %v. Mencoba lagi dalam 5 detik...", err)
				time.Sleep(5 * time.Second)
				continue
			}
			log.Printf("Menerima job baru: Kirim email ke %s", job.To)

			// <-- PERBAIKAN: Mengatasi `errcheck` dengan memeriksa error dari `Send`.
			if err := emailService.Send(job.To, job.Subject, job.Body); err != nil {
				log.Printf("ERROR: Gagal mengirim email untuk job ke %s: %v", job.To, err)
				// Jangan hentikan worker, cukup catat error dan lanjut ke job berikutnya.
			}
		}
	}()

	serviceName := "prism-notification-service"
	log.Printf("Starting %s...", serviceName)

	jaegerEndpoint := os.Getenv("JAEGER_ENDPOINT")
	if jaegerEndpoint == "" {
		jaegerEndpoint = "jaeger:4317"
	}
	tp, err := telemetry.InitTracerProvider(serviceName, jaegerEndpoint)
	if err != nil {
		log.Fatalf("Failed to initialize OTel tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	notificationHandler := handler.NewNotificationHandler(queueService)
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080" // Setiap service berjalan di port 8080 di dalam containernya
	}

	log.Printf("Service configured to run on port %s", portStr)

	// Initialize Gin Router
	router := gin.Default()
	router.Use(otelgin.Middleware(serviceName))
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	// --- Group routes ---
	notificationRoutes := router.Group("/notifications")
	{
		notificationRoutes.POST("/send", notificationHandler.SendNotification)
	}

	ginutil.SetupHealthRoutesForGroup(notificationRoutes, serviceName, "1.0.0")

	log.Printf("Starting %s on port %s", serviceName, portStr)
	if err := router.Run(":" + portStr); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
