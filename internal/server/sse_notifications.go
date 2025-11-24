package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// NotificationHub manages SSE connections for real-time notifications
type NotificationHub struct {
	clients    map[string]*sseClient
	mu         sync.RWMutex
	broadcast  chan *Notification
	register   chan *sseClient
	unregister chan *sseClient
}

// sseClient represents a connected SSE client
type sseClient struct {
	id       string
	user     *auth.User
	messages chan *Notification
	done     chan struct{}
}

// Notification represents a system notification
type Notification struct {
	Type      string                 `json:"type"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data"`
	Timestamp int64                  `json:"timestamp"`
	TenantID  string                 `json:"tenantId,omitempty"`
}

// NewNotificationHub creates a new notification hub
func NewNotificationHub() *NotificationHub {
	hub := &NotificationHub{
		clients:    make(map[string]*sseClient),
		broadcast:  make(chan *Notification, 100),
		register:   make(chan *sseClient),
		unregister: make(chan *sseClient),
	}
	go hub.run()
	return hub
}

// run handles client registration and broadcasting
func (h *NotificationHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()
			logrus.Debugf("SSE client registered: %s (user: %s)", client.id, client.user.Username)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.messages)
			}
			h.mu.Unlock()
			logrus.Debugf("SSE client unregistered: %s", client.id)

		case notification := <-h.broadcast:
			h.mu.RLock()
			clientCount := len(h.clients)
			sentCount := 0
			for _, client := range h.clients {
				// Filter notifications based on user permissions
				if h.shouldReceiveNotification(client.user, notification) {
					select {
					case client.messages <- notification:
						sentCount++
					default:
						// Client is not reading, skip
						logrus.WithField("client_id", client.id).Warn("Client message channel full, skipping notification")
					}
				}
			}
			h.mu.RUnlock()
			logrus.WithFields(logrus.Fields{
				"notification_type": notification.Type,
				"total_clients":     clientCount,
				"sent_to":           sentCount,
			}).Info("Broadcast notification to SSE clients")
		}
	}
}

// shouldReceiveNotification determines if a user should receive a notification
func (h *NotificationHub) shouldReceiveNotification(user *auth.User, notif *Notification) bool {
	// Only admins receive blocked user notifications
	isAdmin := false
	for _, role := range user.Roles {
		if role == "admin" || role == "tenant-admin" {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		return false
	}

	// Global admins see all notifications
	for _, role := range user.Roles {
		if role == "admin" {
			return true
		}
	}

	// Tenant admins only see notifications for their tenant
	if user.TenantID != "" && notif.TenantID == user.TenantID {
		return true
	}

	return false
}

// SendNotification broadcasts a notification to all eligible clients
func (h *NotificationHub) SendNotification(notif *Notification) {
	select {
	case h.broadcast <- notif:
	default:
		logrus.Warn("Notification channel full, dropping notification")
	}
}

// handleNotificationStream handles SSE connections
func (s *Server) handleNotificationStream(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Only admins can connect to notification stream
	isAdmin := false
	for _, role := range user.Roles {
		if role == "admin" || role == "tenant-admin" {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		s.writeError(w, "Forbidden: Only admins can access notifications", http.StatusForbidden)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client with timestamp-based ID
	client := &sseClient{
		id:       fmt.Sprintf("%s-%d", user.ID, time.Now().UnixNano()),
		user:     user,
		messages: make(chan *Notification, 10),
		done:     make(chan struct{}),
	}

	logrus.WithFields(logrus.Fields{
		"user_id":  user.ID,
		"username": user.Username,
		"roles":    user.Roles,
	}).Info("SSE client connecting")

	// Register client
	s.notificationHub.register <- client
	defer func() {
		s.notificationHub.unregister <- client
	}()

	// Get flusher for SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		logrus.Error("Streaming unsupported")
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial connection message
	fmt.Fprintf(w, "data: {\"type\":\"connected\",\"message\":\"Notification stream connected\"}\n\n")
	flusher.Flush()

	// Listen for messages or client disconnect
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-client.messages:
			if !ok {
				return
			}
			data, err := json.Marshal(msg)
			if err != nil {
				logrus.WithError(err).Error("Failed to marshal notification")
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
