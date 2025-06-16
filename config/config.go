package config

import (
	"fmt"
	"log"

	commonconfig "github.com/Lumina-Enterprise-Solutions/prism-common-libs/config"
)

type Config struct {
	Port           int
	ServiceName    string
	JaegerEndpoint string
	RedisAddr      string
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
		RedisAddr:      loader.Get("config/global/redis_addr", "cache-redis:6379"),
	}
}
