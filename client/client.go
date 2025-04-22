// client.go
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/go-vgo/robotgo"
	"github.com/gorilla/websocket"
	hook "github.com/robotn/gohook"
)

type AppState struct {
	Conn     *websocket.Conn
	Hotkey   string
	IsActive bool
}

var (
	state      AppState
	mainWindow fyne.Window
	a          fyne.App
	serverURL  = "ws://localhost:8765/ws"
	connMu     sync.RWMutex
	stateMu    sync.RWMutex
	hookChan   chan hook.Event
	hookMu     sync.Mutex
)

func main() {
	initLogger()
	testMouseControl()
	a = app.New()
	initWindow()

	time.Sleep(300 * time.Millisecond)
	go connectToServer()

	a.Run()
}

func initLogger() {
	file, err := os.OpenFile("client.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal("Ошибка открытия лог-файла:", err)
	}
	log.SetOutput(file)
	log.Println("=== Логгер инициализирован ===")
}

func initWindow() {
	mainWindow = a.NewWindow("ClickSync")
	mainWindow.Resize(fyne.NewSize(300, 150))
	state.Hotkey = "f5"
	showConnectionScreen()

	mainWindow.SetOnClosed(func() {
		hook.End()
		if hookChan != nil {
			close(hookChan)
		}
		if state.Conn != nil {
			state.Conn.Close()
		}
	})
}

func showConnectionScreen() {
	mainWindow.SetContent(container.NewVBox(
		widget.NewLabel("Подключение к серверу..."),
		widget.NewProgressBarInfinite(),
	))
}

// Основные изменения в функциях registerHotkey и connectToServer

func registerHotkey() {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Ошибка регистрации хоткея:", r)
		}
	}()

	hookMu.Lock()
	defer hookMu.Unlock()

	// Завершаем предыдущий хук с гарантией
	if hookChan != nil {
		log.Println("Завершение предыдущего хука")
		hook.End()
		hook.Process(hookChan)
	}

	// Инициализация нового канала
	hookChan = hook.Start()
	log.Printf("Регистрация хоткея: %s (rawcode=116)", state.Hotkey)

	hook.Register(hook.KeyDown, []string{strings.ToLower(state.Hotkey)}, func(e hook.Event) {
		log.Printf("[HOTKEY] Событие: %+v", e)

		// Проверка кода клавиши F5
		if e.Rawcode != 116 {
			log.Printf("[HOTKEY] Игнорируем клавишу: %d", e.Rawcode)
			return
		}

		// Проверка состояния
		stateMu.RLock()
		active := state.IsActive
		conn := state.Conn
		stateMu.RUnlock()

		if !active || conn == nil {
			log.Println("[HOTKEY] Блокировка: неактивно или нет соединения")
			return
		}

		// Выполнение клика
		x, y := robotgo.GetMousePos()
		log.Printf("[HOTKEY] Клик на позиции: %d,%d", x, y)
		performLocalClick()

		// Отправка координат
		screenW, screenH := robotgo.GetScreenSize()
		msg, _ := json.Marshal(map[string]interface{}{
			"type": "sync_click",
			"relX": float64(x) / float64(screenW),
			"relY": float64(y) / float64(screenH),
		})

		// Отправка с таймаутом
		conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			log.Printf("[HOTKEY] Ошибка отправки: %v", err)
			reconnect()
		}
	})
}

func testMouseControl() {
	log.Println("Running mouse diagnostic...")

	// Тест 1: Перемещение мыши
	robotgo.Move(100, 100)
	x, y := robotgo.GetMousePos()
	log.Printf("Mouse position after move: X=%d, Y=%d", x, y)

	// Тест 2: Клик без задержек
	robotgo.MouseClick("left", false)
	log.Println("Immediate click executed")

	// Тест 3: Клик с эмуляцией человека
	robotgo.MilliSleep(100)
	robotgo.MouseDown("left")
	robotgo.MilliSleep(50)
	robotgo.MouseUp("left")
	log.Println("Human-like click executed")
}

func performLocalClick() {
	log.Println("Начало эмуляции клика...")

	robotgo.MilliSleep(50)
	robotgo.MouseDown("left")
	log.Println("Левая кнопка мыши нажата")

	robotgo.MilliSleep(30)
	robotgo.MouseUp("left")
	log.Println("Левая кнопка мыши отпущена")

	x, y := robotgo.GetMousePos()
	log.Printf("Текущая позиция мыши: X=%d, Y=%d", x, y)
}

// Остальные функции без изменений (showErrorScreen, connectToServer, showMainUI, readMessages, handleRemoteClick, reconnect, saveHotkey)

func handleRemoteClick(relX, relY float64) {
	log.Printf("[DEBUG] Handling remote click: relX=%.2f, relY=%.2f", relX, relY)

	screenW, screenH := robotgo.GetScreenSize()
	log.Printf("[DEBUG] Screen size: %dx%d", screenW, screenH)

	x := int(relX * float64(screenW))
	y := int(relY * float64(screenH))

	log.Printf("[DEBUG] Moving to: X=%d, Y=%d", x, y)
	robotgo.Move(x, y)

	// Добавьте визуальную обратную связь
	robotgo.MilliSleep(100)
	robotgo.MouseClick("left", true)
	log.Println("[DEBUG] Remote click executed")
}

func showErrorScreen() {
	fyne.Do(func() {
		mainWindow.SetContent(container.NewVBox(
			widget.NewLabel("Ошибка подключения к серверу"),
			widget.NewButton("Повторить подключение", func() {
				go connectToServer()
			}),
		))
	})
}

func connectToServer() {
	defer func() {
		if r := recover(); r != nil {
			log.Println("Паника при подключении:", r)
		}
	}()

	connMu.Lock()
	defer connMu.Unlock()

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	// Устанавливаем соединение с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := dialer.DialContext(ctx, serverURL, nil)
	if err != nil {
		log.Printf("Ошибка подключения: %v", err)
		fyne.Do(showErrorScreen)
		return
	}

	// Настройка соединения
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("Закрытие соединения: %d - %s", code, text)
		reconnect()
		return nil
	})
	conn.SetPingHandler(func(msg string) error {
		log.Println("[WS] Получен Ping")
		return nil
	})

	conn.SetPongHandler(func(msg string) error {
		log.Println("[WS] Получен Pong")
		return nil
	})

	// Запуск keep-alive
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Println("[WS] Keep-alive failed")
					reconnect()
					return
				}
			}
		}
	}()
	// Обновление состояния
	stateMu.Lock()
	state.Conn = conn
	state.IsActive = true
	stateMu.Unlock()

	// Принудительная перерисовка UI
	fyne.Do(func() {
		showMainUI()
		registerHotkey() // Немедленная регистрация хоткея
	})

	go readMessages()
}

func showMainUI() {
	hotkeyEntry := widget.NewEntry()
	hotkeyEntry.SetText(state.Hotkey)
	hotkeyEntry.Disable()

	keyMap := map[fyne.KeyName]string{
		fyne.KeyF1:     "f1",
		fyne.KeyF2:     "f2",
		fyne.KeyF3:     "f3",
		fyne.KeyF4:     "f4",
		fyne.KeyF5:     "f5",
		fyne.KeyEscape: "escape",
		fyne.KeyEnter:  "enter",
	}

	captureButton := widget.NewButton("Захватить клавишу", func() {
		mainWindow.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
			keyStr, ok := keyMap[e.Name]
			if !ok {
				keyStr = strings.ToLower(string(e.Name))
			}
			hotkeyEntry.SetText(keyStr)
			mainWindow.Canvas().SetOnTypedKey(nil)
		})
	})

	saveButton := widget.NewButton("Сохранить", func() {
		newHotkey := strings.ToLower(hotkeyEntry.Text)
		if !saveHotkey(newHotkey) {
			log.Printf("Недопустимый хоткей: %s", newHotkey)
			return
		}
		if newHotkey != "" {
			stateMu.Lock()
			state.Hotkey = newHotkey
			stateMu.Unlock()
			fyne.Do(func() {
				time.Sleep(1 * time.Second)
				stateMu.Lock()
				state.IsActive = true
				stateMu.Unlock()
				registerHotkey()
			})
		}
	})

	fyne.Do(func() {
		mainWindow.SetContent(container.NewVBox(
			container.NewHBox(
				widget.NewIcon(theme.ConfirmIcon()),
				widget.NewLabel("Статус: Активно"),
			),
			widget.NewSeparator(),
			widget.NewLabel("Горячая клавиша:"),
			hotkeyEntry,
			captureButton,
			saveButton,
		))
		mainWindow.Show()
	})
}

func readMessages() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[CRITICAL] Panic in readMessages: %v", r)
		}
	}()

	for {
		_, msg, err := state.Conn.ReadMessage()
		if err != nil {
			log.Printf("[ERROR] Read error: %v", err)
			reconnect()
			return
		}

		log.Printf("[DEBUG] Received message: %s", string(msg))
		var data map[string]interface{}
		if err := json.Unmarshal(msg, &data); err != nil {
			continue
		}

		if data["type"] == "execute_click" {
			relX, _ := data["relX"].(float64)
			relY, _ := data["relY"].(float64)
			handleRemoteClick(relX, relY)
		}
	}
}

func reconnect() {
	connMu.Lock()
	defer connMu.Unlock()

	stateMu.Lock()
	state.IsActive = false
	state.Conn = nil
	stateMu.Unlock()

	go func() {
		time.Sleep(2 * time.Second)
		connectToServer()
	}()
}

func saveHotkey(newHotkey string) bool {
	validKeys := map[string]bool{
		"f1": true, "f2": true, "f3": true, "f4": true, "f5": true,
		"escape": true, "enter": true,
	}
	return validKeys[strings.ToLower(newHotkey)]
}
