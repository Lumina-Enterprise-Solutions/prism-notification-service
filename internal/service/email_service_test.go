package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewEmailService_WithCredentials menguji pembuatan service saat env var tersedia.
func TestNewEmailService_WithCredentials(t *testing.T) {
	// ARRANGE: Atur environment variable khusus untuk test ini.
	// t.Setenv adalah cara aman untuk mengatur env var yang akan otomatis
	// dibersihkan setelah test selesai.
	t.Setenv("MAILTRAP_HOST", "sandbox.smtp.mailtrap.io")
	t.Setenv("MAILTRAP_PORT", "2525")
	t.Setenv("MAILTRAP_USER", "testuser")
	t.Setenv("MAILTRAP_PASS", "testpass")

	// ACT: Panggil fungsi yang ingin diuji.
	// Kita perlu menggunakan require.NotPanics karena jika template tidak ditemukan,
	// NewEmailService akan memanggil log.Fatalf yang menyebabkan panic.
	// Untuk test ini, kita asumsikan template ada.
	// Pastikan direktori 'templates' ada relatif terhadap root proyek saat menjalankan tes.
	var service *EmailService
	require.NotPanics(t, func() {
		service = NewEmailService()
	}, "NewEmailService tidak seharusnya panic jika template ada")

	// ASSERT: Verifikasi hasilnya.
	require.NotNil(t, service, "Service tidak boleh nil")
	assert.NotNil(t, service.dialer, "Dialer harus diinisialisasi saat kredensial ada")
	assert.NotNil(t, service.templates, "Templates harus di-load")
}

// TestNewEmailService_WithoutCredentials menguji fallback jika env var tidak ada.
func TestNewEmailService_WithoutCredentials(t *testing.T) {
	// ARRANGE: Pastikan tidak ada env var yang di-set (default state).

	// ACT
	var service *EmailService
	require.NotPanics(t, func() {
		service = NewEmailService()
	})

	// ASSERT
	require.NotNil(t, service)
	assert.Nil(t, service.dialer, "Dialer seharusnya nil jika kredensial tidak ada")
}

// TestEmailService_Send_TemplateNotFound menguji penanganan error jika template tidak ada.
func TestEmailService_Send_TemplateNotFound(t *testing.T) {
	// ARRANGE
	// Inisialisasi service dengan dialer (agar tidak masuk ke mode simulasi)
	t.Setenv("MAILTRAP_HOST", "host")
	t.Setenv("MAILTRAP_PORT", "123")
	t.Setenv("MAILTRAP_USER", "user")
	t.Setenv("MAILTRAP_PASS", "pass")

	var service *EmailService
	require.NotPanics(t, func() {
		service = NewEmailService()
	})
	require.NotNil(t, service.dialer)

	// ACT: Coba kirim email dengan nama template yang tidak ada.
	err := service.Send("test@example.com", "Subjek", "template_tidak_ada.html", nil)

	// ASSERT: Verifikasi bahwa kita mendapatkan error yang berhubungan dengan template.
	assert.Error(t, err, "Fungsi Send seharusnya mengembalikan error")
	assert.Contains(t, err.Error(), "template_tidak_ada.html", "Pesan error harus menyebutkan nama template yang hilang")
}

// TestEmailService_Send_Simulated menguji mode simulasi saat dialer nil.
func TestEmailService_Send_Simulated(t *testing.T) {
	// ARRANGE
	// Buat service tanpa kredensial
	var service *EmailService
	require.NotPanics(t, func() {
		service = NewEmailService()
	})
	require.Nil(t, service.dialer)

	// ACT
	// Karena dialer nil, fungsi ini seharusnya hanya mencetak log dan mengembalikan nil
	err := service.Send("test@example.com", "Subjek", "welcome.html", nil)

	// ASSERT
	assert.NoError(t, err, "Mode simulasi seharusnya tidak mengembalikan error")
}
