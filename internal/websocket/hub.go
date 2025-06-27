package websocket

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type WSConnection interface {
	WriteJSON(v interface{}) error
}

type Hub struct {
	clients    map[string]*Client
	mu         sync.RWMutex
	register   chan *Client
	unregister chan *Client
	stop       chan struct{}
}

type Client struct {
	UserID string
	Conn   WSConnection
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[string]*Client),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		stop:       make(chan struct{}),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			log.Printf("WebSocket client registered: UserID=%s", client.UserID)
			h.clients[client.UserID] = client
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.UserID]; ok {
				delete(h.clients, client.UserID)
				log.Printf("WebSocket client unregistered: UserID=%s", client.UserID)
			}
			h.mu.Unlock()
		case <-h.stop:
			log.Println("WebSocket Hub has been stopped.")
			return
		}
	}
}

func (h *Hub) Stop() {
	close(h.stop)
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	// FIX: Hapus titik yang salah ketik.
	h.unregister <- client
}

func (h *Hub) IsClientRegistered(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.clients[userID]
	return ok
}

func (h *Hub) SendToUser(userID string, message interface{}) bool {
	h.mu.RLock()
	client, ok := h.clients[userID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	err := client.Conn.WriteJSON(message)
	if err != nil {
		log.Printf("Error writing WebSocket message to user %s: %v", userID, err)
		return false
	}

	return true
}

func (h *Hub) Broadcast(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for userID, client := range h.clients {
		if conn, ok := client.Conn.(*websocket.Conn); ok {
			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("Error broadcasting to user %s: %v", userID, err)
			}
		}
	}
}
