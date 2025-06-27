package websocket

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockConn adalah implementasi tiruan dari interface WSConnection kita.
type mockConn struct {
	// Channel untuk "menerima" data yang ditulis, agar kita bisa memverifikasinya.
	writeJSONChan chan interface{}
}

// WriteJSON memenuhi interface WSConnection.
func (m *mockConn) WriteJSON(v interface{}) error {
	m.writeJSONChan <- v
	return nil
}

// newMockConn adalah helper untuk membuat mock baru.
func newMockConn() *mockConn {
	return &mockConn{
		// Buffer 1 agar pengiriman tidak memblokir test.
		writeJSONChan: make(chan interface{}, 1),
	}
}

func TestHub_RegisterAndUnregister(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	go hub.Run()

	// Kita tidak perlu koneksi nyata, cukup struct kosong yang memenuhi interface.
	client := &Client{
		UserID: "user-1",
		Conn:   &mockConn{},
	}

	// Test Register
	hub.Register(client)
	time.Sleep(50 * time.Millisecond)
	hub.mu.RLock()
	_, ok := hub.clients["user-1"]
	hub.mu.RUnlock()
	assert.True(t, ok, "Client seharusnya terdaftar")

	// Test Unregister
	hub.Unregister(client)
	time.Sleep(50 * time.Millisecond)
	hub.mu.RLock()
	_, ok = hub.clients["user-1"]
	hub.mu.RUnlock()
	assert.False(t, ok, "Client seharusnya tidak terdaftar lagi")
}

func TestHub_SendToUser(t *testing.T) {
	hub := NewHub()
	defer hub.Stop()
	go hub.Run()

	// Buat koneksi tiruan yang bisa kita periksa.
	mock := newMockConn()
	client := &Client{UserID: "user-2", Conn: mock}

	hub.Register(client)
	time.Sleep(50 * time.Millisecond)

	// ACT: Kirim pesan melalui hub.
	message := map[string]string{"data": "hello"}
	sent := hub.SendToUser(client.UserID, message)
	assert.True(t, sent, "SendToUser seharusnya berhasil")

	// ASSERT: Verifikasi bahwa metode WriteJSON di mock kita dipanggil dengan data yang benar.
	select {
	case received := <-mock.writeJSONChan:
		assert.Equal(t, message, received, "Pesan yang dikirim ke koneksi tidak cocok")
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for message to be sent via mock connection")
	}
}
