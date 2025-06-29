package config

import (
	"fmt"
	"log"
	"os"

	commonconfig "github.com/Lumina-Enterprise-Solutions/prism-common-libs/config"
)

type Config struct {
	Port           int
	ServiceName    string
	JaegerEndpoint string
	RabbitMQURL    string
	RedisAddr      string
	VaultAddr      string
	VaultToken     string
}

func Load() *Config {
	loader, err := commonconfig.NewLoader()
	if err != nil {
		log.Fatalf("Gagal membuat config loader: %v", err)
	}

	serviceName := "prism-notification-service"

	return &Config{
		Port:           loader.GetInt(fmt.Sprintf("config/%s/port", serviceName), 8080),
		ServiceName:    serviceName,
		JaegerEndpoint: loader.Get("config/global/jaeger_endpoint", "jaeger:4317"),
		RabbitMQURL:    os.Getenv("RABBITMQ_URL"),
		RedisAddr:      loader.Get("config/global/redis_addr", "cache-redis:6379"),
		VaultAddr:      os.Getenv("VAULT_ADDR"), // Env var masih cara terbaik untuk info infra
		VaultToken:     os.Getenv("VAULT_TOKEN"),
	}
}
