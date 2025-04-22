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
	CheckOrigin:      func(r *http.Request) bool { return true },
	HandshakeTimeout: 60 * time.Second,
	ReadBufferSize:   1024,
	WriteBufferSize:  1024,
}

type Client struct {
	Conn    *websocket.Conn
	ID      string
	Hotkeys map[string]string
}
type Command struct {
	Type string  `json:"type"`
	RelX float64 `json:"relX"`
	RelY float64 `json:"relY"`
}

var (
	clients   = make(map[string]*Client)
	clientsMu sync.Mutex
)

func main() {
	http.HandleFunc("/ws", wsHandler)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("WebSocket Server"))
	})

	log.Println("Сервер запущен на :8765")
	log.Fatal(http.ListenAndServe(":8765", nil))
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, _ := upgrader.Upgrade(w, r, nil)
	defer conn.Close()

	client := &Client{
		Conn: conn,
		ID:   uuid.New().String(),
	}

	clientsMu.Lock()
	clients[client.ID] = client
	clientsMu.Unlock()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var data map[string]interface{}
		json.Unmarshal(msg, &data)

		if data["type"] == "sync_click" {
			broadcastClick(data)
		}
	}
}

func broadcastClick(data map[string]interface{}) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	log.Printf("[SERVER] Broadcasting click: %v", data)

	for _, client := range clients {
		client.Conn.WriteJSON(map[string]interface{}{
			"type": "execute_click",
			"relX": data["relX"],
			"relY": data["relY"],
		})
	}
}

func updateClientHotkeys(c *Client, data map[string]interface{}) {
	action := data["action"].(string)
	key := data["key"].(string)
	c.Hotkeys[action] = key
}
func handleCommand(client *Client, data map[string]interface{}) {
	switch data["type"] {
	case "update_hotkey":
		action, ok1 := data["action"].(string)
		key, ok2 := data["key"].(string)
		if ok1 && ok2 {
			client.Hotkeys[action] = key
			log.Printf("Обновление хоткея: %s -> %s", action, key)
			broadcastHotkeyUpdate(client, action, key)
		}
	case "relative_command":
		relX, _ := data["relX"].(float64)
		relY, _ := data["relY"].(float64)
		handleRelativeCommand(relX, relY)
	}
}
func broadcastHotkeyUpdate(sender *Client, action, key string) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

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
func handleRelativeCommand(relX, relY float64) {
	log.Printf("[СЕРВЕР] Получена команда: relX=%.2f, relY=%.2f", relX, relY)
	clientsMu.Lock()
	defer clientsMu.Unlock()

	for id, client := range clients {
		log.Printf("[СЕРВЕР] Отправка клиенту %s: X=%.2f, Y=%.2f", id, relX, relY)
		err := client.Conn.WriteJSON(map[string]interface{}{
			"type": "relative_click",
			"relX": relX,
			"relY": relY,
		})
		if err != nil {
			log.Printf("[СЕРВЕР] Ошибка отправки клиенту %s: %v", id, err)
		}
	}
}
