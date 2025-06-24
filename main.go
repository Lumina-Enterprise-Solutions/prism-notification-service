package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	notifconfig "github.com/Lumina-Enterprise-Solutions/prism-notification-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	"github.com/gin-gonic/gin"
	ginprometheus "github.com/zsais/go-gin-prometheus"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func setupDependencies(cfg *notifconfig.Config) error {
	vaultClient, err := client.NewVaultClient(cfg.VaultAddr, cfg.VaultToken)
	if err != nil {
		return fmt.Errorf("gagal membuat klien Vault: %w", err)
	}
	secretPath := "secret/data/prism"
	requiredSecrets := []string{
		"mailtrap_host",
		"mailtrap_port",
		"mailtrap_user",
		"mailtrap_pass",
	}
	if err := vaultClient.LoadSecretsToEnv(secretPath, requiredSecrets...); err != nil {
		return fmt.Errorf("gagal memuat kredensial Mailtrap dari Vault: %w", err)
	}
	log.Println("Berhasil memuat kredensial Mailtrap dari Vault.")
	return nil
}

func main() {
	cfg := notifconfig.Load()
	log.Printf("Konfigurasi dimuat: ServiceName=%s, Port=%d, Jaeger=%s, Redis=%s", cfg.ServiceName, cfg.Port, cfg.JaegerEndpoint, cfg.RedisAddr)

	tp, err := telemetry.InitTracerProvider(cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatalf("Gagal menginisialisasi OTel tracer provider: %v", err)
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Printf("Error saat mematikan tracer provider: %v", err)
		}
	}()

	if err := setupDependencies(cfg); err != nil {
		log.Fatalf("Gagal menginisialisasi dependensi: %v", err)
	}

	emailService := service.NewEmailService()
	queueService := service.NewQueueService(cfg.RedisAddr)

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
			if err := emailService.Send(job.To, job.Subject, job.Body); err != nil {
				log.Printf("ERROR: Gagal mengirim email untuk job ke %s: %v", job.To, err)
			}
		}
	}()

	notificationHandler := handler.NewNotificationHandler(queueService)
	portStr := strconv.Itoa(cfg.Port)

	router := gin.Default()
	router.Use(otelgin.Middleware(cfg.ServiceName))
	p := ginprometheus.NewPrometheus("gin")
	p.Use(router)

	notificationRoutes := router.Group("/notifications")
	{
		notificationRoutes.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "healthy"}) })
		notificationRoutes.POST("/send", notificationHandler.SendNotification)
	}

	consulClient, err := client.RegisterService(client.ServiceRegistrationInfo{
		ServiceName:    cfg.ServiceName,
		ServiceID:      fmt.Sprintf("%s-%s", cfg.ServiceName, portStr),
		Port:           cfg.Port,
		HealthCheckURL: fmt.Sprintf("http://%s:%s/notifications/health", cfg.ServiceName, portStr),
	})
	if err != nil {
		log.Fatalf("Gagal mendaftarkan service ke Consul: %v", err)
	}
	defer client.DeregisterService(consulClient, fmt.Sprintf("%s-%s", cfg.ServiceName, portStr))

	log.Printf("Memulai %s di port %s", cfg.ServiceName, portStr)
	if err := router.Run(":" + portStr); err != nil {
		log.Fatalf("Gagal menjalankan server: %v", err)
	}
}
