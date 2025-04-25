package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Client struct {
	Conn     *websocket.Conn
	ID       string
	LastSeen time.Time
	Hotkeys  map[string]string
}

var (
	clients   = make(map[string]*Client)
	clientsMu sync.RWMutex
)

func main() {
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("WebSocket Server"))
	})

	go cleanupClients()

	log.Println("Сервер запущен на :8765")
	log.Fatal(http.ListenAndServe(":8765", nil))
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Ошибка подключения: %v", err)
		return
	}

	client := &Client{
		Conn:     conn,
		ID:       uuid.New().String(),
		LastSeen: time.Now(),
		Hotkeys:  make(map[string]string),
	}

	clientsMu.Lock()
	clients[client.ID] = client
	clientsMu.Unlock()

	log.Printf("Новое подключение: %s", client.ID)

	defer func() {
		clientsMu.Lock()
		delete(clients, client.ID)
		clientsMu.Unlock()
		conn.Close()
	}()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Отключение клиента %s: %v", client.ID, err)
			break
		}

		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}

		switch data["type"] {
		case "sync_click":
			handleSyncClick(client, data)
		case "ping":
			client.LastSeen = time.Now()
		case "update_hotkey":
			handleHotkeyUpdate(client, data)
		}
	}
}
func handleSyncClick(sender *Client, data map[string]interface{}) {
	relX, _ := data["relX"].(float64)
	relY, _ := data["relY"].(float64)

	msg := map[string]interface{}{
		"type": "execute_click",
		"relX": relX,
		"relY": relY,
	}

	clientsMu.RLock()
	defer clientsMu.RUnlock()

	for _, client := range clients {
		if client.ID == sender.ID {
			continue
		}

		// Исправлено: убрана лишняя скобка
		client.Conn.SetWriteDeadline(time.Now().Add(2 * time.Second))

		// Исправлено: используем локальную переменную client из цикла
		if err := client.Conn.WriteJSON(msg); err != nil {
			log.Printf("Ошибка отправки клиенту %s: %v", client.ID, err)
			client.Conn.Close()
			delete(clients, client.ID)
		}
	}
}

func handleHotkeyUpdate(client *Client, data map[string]interface{}) {
	action, ok1 := data["action"].(string)
	key, ok2 := data["key"].(string)
	if ok1 && ok2 {
		client.Hotkeys[action] = key
		broadcastHotkeyUpdate(client, action, key)
	}
}

func broadcastHotkeyUpdate(sender *Client, action, key string) {
	clientsMu.RLock()
	defer clientsMu.RUnlock()

	for _, client := range clients {
		if client.ID != sender.ID {
			msg := map[string]interface{}{
				"type":   "hotkey_update",
				"action": action,
				"key":    key,
			}
			client.Conn.WriteJSON(msg)
		}
	}
}

func cleanupClients() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		clientsMu.Lock()
		for id, client := range clients {
			if time.Since(client.LastSeen) > 2*time.Minute {
				client.Conn.Close()
				delete(clients, id)
				log.Printf("Удален неактивный клиент: %s", id)
			}
		}
		clientsMu.Unlock()
	}
}
