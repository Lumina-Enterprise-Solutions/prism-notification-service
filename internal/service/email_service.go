package service

import (
	"log"
	"os"
	"strconv"

	"gopkg.in/gomail.v2"
)

type EmailService struct {
	dialer *gomail.Dialer
}

func NewEmailService() *EmailService {
	// Muat konfigurasi dari environment (yang di-set dari Vault)
	host := os.Getenv("MAILTRAP_HOST")
	port, _ := strconv.Atoi(os.Getenv("MAILTRAP_PORT"))
	user := os.Getenv("MAILTRAP_USER")
	pass := os.Getenv("MAILTRAP_PASS")

	if host == "" {
		log.Println("PERINGATAN: Kredensial Mailtrap tidak diset. Email tidak akan terkirim.")
		return &EmailService{dialer: nil}
	}

	dialer := gomail.NewDialer(host, port, user, pass)
	return &EmailService{dialer: dialer}
}

func (s *EmailService) Send(to, subject, body string) error {
	if s.dialer == nil {
		log.Printf("Email Service tidak terkonfigurasi. Mensimulasikan pengiriman email ke %s\n", to)
		return nil // Jangan return error agar tidak mengganggu alur
	}

	m := gomail.NewMessage()
	m.SetHeader("From", "no-reply@prismerp.com")
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body) // Bisa juga "text/plain"

	log.Printf("Mengirim email ke %s via Mailtrap...", to)
	return s.dialer.DialAndSend(m)
}
