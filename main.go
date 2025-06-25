package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
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
	// === Tahap 2: Jalankan Worker & Server ===

	// Konteks untuk mengontrol siklus hidup worker
	workerCtx, workerCancel := context.WithCancel(context.Background())

	go func() {
		log.Info().Msg("Starting background worker for notification queue...")
		const maxRetries = 3
		const retryDelay = 30 * time.Second

		for {
			select {
			case <-workerCtx.Done():
				log.Info().Msg("Notification worker stopping...")
				return
			default:
				job, err := queueService.Dequeue(workerCtx)
				if err != nil {
					if errors.Is(err, context.Canceled) {
						continue
					}
					log.Error().Err(err).Msg("Failed to dequeue job, retrying...")
					time.Sleep(5 * time.Second) // Tunggu sebentar jika Redis error
					continue
				}

				log.Info().Str("recipient", job.To).Msg("Processing new job")

				// --- Logika Retry ---
				var sendErr error
				for i := 0; i < maxRetries; i++ {
					sendErr = emailService.Send(job.To, job.Subject, job.Body)
					if sendErr == nil {
						log.Info().Str("recipient", job.To).Msg("Email sent successfully")
						break // Berhasil, keluar dari loop retry
					}
					log.Warn().
						Err(sendErr).
						Str("recipient", job.To).
						Int("attempt", i+1).
						Int("max_attempts", maxRetries).
						Msg("Failed to send email, will retry...")

					// Tunggu sebelum mencoba lagi, kecuali ini percobaan terakhir
					if i < maxRetries-1 {
						time.Sleep(retryDelay)
					}
				}

				// Jika setelah semua retry masih gagal, pindahkan ke DLQ
				if sendErr != nil {
					log.Error().
						Err(sendErr).
						Str("recipient", job.To).
						Msg("Job failed after all retries, moving to DLQ")
					if dlqErr := queueService.EnqueueToDLQ(context.Background(), *job); dlqErr != nil {
						log.Error().Err(dlqErr).Str("recipient", job.To).Msg("FATAL: Failed to move job to DLQ")
					}
				}
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

	srv := &http.Server{
		Addr:    ":" + portStr,
		Handler: router,
	}

	// Jalankan server HTTP di goroutine
	go func() {
		log.Info().Str("service", cfg.ServiceName).Msgf("HTTP server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("HTTP server ListenAndServe error")
		}
	}()

	// === Tahap 3: Tangani Shutdown ===
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("Shutdown signal received, starting graceful shutdown...")

	// Batalkan konteks worker agar berhenti
	workerCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server forced to shutdown")
	}

	log.Info().Msg("Server exiting gracefully.")
}
