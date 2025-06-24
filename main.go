package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/client"
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/logger" // <-- Impor logger baru
	"github.com/Lumina-Enterprise-Solutions/prism-common-libs/telemetry"
	notifconfig "github.com/Lumina-Enterprise-Solutions/prism-notification-service/config"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/handler"
	"github.com/Lumina-Enterprise-Solutions/prism-notification-service/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log" // <-- Impor log dari zerolog
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
	log.Info().Msg("Kredensial Mailtrap berhasil dimuat dari Vault.")
	return nil
}

func main() {
	logger.Init()
	cfg := notifconfig.Load()
	log.Info().
		Str("service", cfg.ServiceName).
		Int("port", cfg.Port).
		Str("jaeger_endpoint", cfg.JaegerEndpoint).
		Str("redis_addr", cfg.RedisAddr).
		Msg("Configuration loaded")
	tp, err := telemetry.InitTracerProvider(cfg.ServiceName, cfg.JaegerEndpoint)
	if err != nil {
		log.Fatal().Err(err).Msg("Gagal menginisialisasi OTel tracer provider")
	}
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			log.Error().Err(err).Msg("Error saat mematikan tracer provider")
		}
	}()

	if err := setupDependencies(cfg); err != nil {
		log.Fatal().Err(err).Msg("Gagal menginisialisasi dependensi")
	}

	emailService := service.NewEmailService()
	queueService := service.NewQueueService(cfg.RedisAddr)

	go func() {
		log.Info().Msg("Memulai background worker untuk antrian notifikasi...")
		for {
			job, err := queueService.Dequeue(context.Background())
			if err != nil {
				log.Error().Err(err).Msg("Gagal mengambil job dari antrian")
				log.Info().Msg("Mencoba lagi dalam 5 detik...")
				time.Sleep(5 * time.Second)
				continue
			}
			log.Info().Msgf("Menerima job baru: Kirim email ke %s", job.To)
			if err := emailService.Send(job.To, job.Subject, job.Body); err != nil {
				log.Error().Err(err).Msgf("Gagal mengirim email untuk job ke %s", job.To)
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
		log.Fatal().Err(err).Msg("Gagal mendaftarkan service ke Consul")
	}
	defer client.DeregisterService(consulClient, fmt.Sprintf("%s-%s", cfg.ServiceName, portStr))

	log.Info().Msgf("Memulai %s di port %s", cfg.ServiceName, portStr)
	if err := router.Run(":" + portStr); err != nil {
		log.Fatal().Err(err).Msg("Gagal menjalankan server")
	}
}
