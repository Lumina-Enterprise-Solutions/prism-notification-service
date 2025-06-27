package service

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/gomail.v2"
)

type EmailService struct {
	dialer    *gomail.Dialer
	templates *template.Template
}

func NewEmailService() *EmailService {
	host := os.Getenv("MAILTRAP_HOST")
	port, _ := strconv.Atoi(os.Getenv("MAILTRAP_PORT"))
	user := os.Getenv("MAILTRAP_USER")
	pass := os.Getenv("MAILTRAP_PASS")

	// Return mock/dummy service jika kredensial tidak ada.
	if host == "" {
		log.Println("PERINGATAN: Kredensial Mailtrap tidak diset. Email akan disimulasikan (tidak terkirim).")
		// Tetap load template agar bisa diuji terpisah.
		tpl, _ := loadTemplates()
		return &EmailService{dialer: nil, templates: tpl}
	}

	dialer := gomail.NewDialer(host, port, user, pass)
	templates, err := loadTemplates()
	if err != nil {
		log.Fatalf("Gagal memuat template email: %v", err)
	}

	return &EmailService{
		dialer:    dialer,
		templates: templates,
	}
}

// loadTemplates adalah helper untuk mencari dan mem-parse template.
func loadTemplates() (*template.Template, error) {
	// FIX: Cari direktori 'templates' dari path saat ini hingga ke atas.
	// Ini membuat loading template lebih andal di berbagai lingkungan (dev, test, prod).
	var templateDir string
	path, _ := os.Getwd()
	for i := 0; i < 5; i++ { // Batasi pencarian hingga 5 level ke atas
		if _, err := os.Stat(filepath.Join(path, "templates")); err == nil {
			templateDir = filepath.Join(path, "templates")
			break
		}
		path = filepath.Dir(path)
	}

	if templateDir == "" {
		return nil, fmt.Errorf("direktori 'templates' tidak ditemukan")
	}

	log.Printf("Memuat template dari direktori: %s", templateDir)
	return template.ParseGlob(filepath.Join(templateDir, "*.html"))
}

func (s *EmailService) Send(to, subject, templateName string, data interface{}) error {
	if s.dialer == nil {
		log.Printf("Mode Simulasi: Mengirim email '%s' ke %s", templateName, to)
		return nil
	}

	if s.templates == nil {
		return fmt.Errorf("templates tidak diinisialisasi dengan benar")
	}

	var body bytes.Buffer
	err := s.templates.ExecuteTemplate(&body, templateName, data)
	if err != nil {
		return fmt.Errorf("gagal mengeksekusi template %s: %w", templateName, err)
	}

	m := gomail.NewMessage()
	m.SetHeader("From", "no-reply@prismerp.com")
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", body.String())

	log.Printf("Mengirim email dengan template '%s' ke %s...", templateName, to)
	return s.dialer.DialAndSend(m)
}
