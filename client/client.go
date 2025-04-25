package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
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

// AppState хранит состояние приложения
type AppState struct {
	Conn     *websocket.Conn
	Hotkey   string
	IsActive bool
}

const appID = "com.example.clicksync"

var (
	state      AppState
	mainWindow fyne.Window
	a          fyne.App
	serverURL  = "ws://localhost:8765/ws"

	stateMu sync.RWMutex

	// Для обработки глобальных событий клавиатуры (gohook)
	hookMu     sync.Mutex
	hookEvents chan hook.Event

	// Используем context для остановки работы хука
	stopHookCtx    context.Context
	stopHookCancel context.CancelFunc

	hookWG      sync.WaitGroup
	netWG       sync.WaitGroup
	cleanupOnce sync.Once
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL PANIC: %v", r)
		}
	}()

	initLogger()
	a = app.NewWithID(appID)
	initWindow()
	defer cleanup()

	// Обработка сигналов завершения (SIGINT, SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		cleanup()
		os.Exit(0)
	}()

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
	mainWindow.Resize(fyne.NewSize(300, 200))
	// Изначально горячая клавиша — F5
	state.Hotkey = "f5"

	mainWindow.SetCloseIntercept(func() {
		log.Println("Запрос на закрытие приложения")
		cleanup()
		a.Quit()
	})
	showConnectionScreen()
}

func cleanup() {
	log.Println("Запуск очистки ресурсов")
	cleanupOnce.Do(func() {
		stateMu.Lock()
		if state.Conn != nil {
			state.Conn.Close()
			state.Conn = nil
		}
		state.IsActive = false
		stateMu.Unlock()

		hookMu.Lock()
		if stopHookCancel != nil {
			stopHookCancel()
			stopHookCancel = nil
		}
		stopHookCtx = nil
		hookMu.Unlock()

		hookWG.Wait()
		log.Println("Все ресурсы очищены")
	})
}

func showConnectionScreen() {
	fyne.Do(func() {
		mainWindow.SetContent(container.NewVBox(
			widget.NewLabel("Подключение к серверу..."),
			widget.NewProgressBarInfinite(),
		))
		mainWindow.Show()
	})
}

// getVKFromKey возвращает виртуальный код для указанной клавиши.
// Поддерживаются как Windows (vkMapping), так и Linux (linuxMapping) – выбор определяется функцией isLinux().
func getVKFromKey(key string) (uint16, bool) {
	key = strings.ToLower(key)

	// Карта виртуальных кодов для Windows
	var vkMapping = map[string]uint16{
		"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
		"f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
		"f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
		"a": 0x41, "b": 0x42, "c": 0x43, "d": 0x44, "e": 0x45,
		"f": 0x46, "g": 0x47, "h": 0x48, "i": 0x49, "j": 0x4A,
		"k": 0x4B, "l": 0x4C, "m": 0x4D, "n": 0x4E, "o": 0x4F,
		"p": 0x50, "q": 0x51, "r": 0x52, "s": 0x53, "t": 0x54,
		"u": 0x55, "v": 0x56, "w": 0x57, "x": 0x58, "y": 0x59, "z": 0x5A,
		"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34,
		"5": 0x35, "6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,
		"/": 0xBF, ".": 0xBE, ",": 0xBC, "'": 0xDE,
		"[": 0xDB, "]": 0xDD, "\\": 0xDC, ";": 0xBA,
		"=": 0xBB, "-": 0xBD,
		"leftcontrol": 0xA2, "rightcontrol": 0xA3,
		"leftshift": 0xA0, "rightshift": 0xA1,
		"leftalt": 0xA4, "rightalt": 0xA5,
		"capslock": 0x14, "esc": 0x1B,
		"tab": 0x09, "space": 0x20,
	}

	// Карта кодов для Linux (пример – значения могут отличаться в зависимости от системы и используемого X11)
	var linuxMapping = map[string]uint16{
		"f1": 0xBE, "f2": 0xBF, "f3": 0xC0, "f4": 0xC1,
		"f5": 0xC2, "f6": 0xC3, "f7": 0xC4, "f8": 0xC5,
		"f9": 0xC6, "f10": 0xC7, "f11": 0xC8, "f12": 0xC9,
		"a": 0x26, "b": 0x27, "c": 0x28, "d": 0x29, "e": 0x2A,
		"f": 0x2B, "g": 0x2C, "h": 0x2D, "i": 0x2E, "j": 0x2F,
		"k": 0x30, "l": 0x31, "m": 0x32, "n": 0x33, "o": 0x34,
		"p": 0x35, "q": 0x36, "r": 0x37, "s": 0x38, "t": 0x39,
		"u": 0x3A, "v": 0x3B, "w": 0x3C, "x": 0x3D, "y": 0x3E, "z": 0x3F,
		"0": 0x18, "1": 0x19, "2": 0x1A, "3": 0x1B, "4": 0x1C,
		"5": 0x1D, "6": 0x1E, "7": 0x1F, "8": 0x20, "9": 0x21,
		"/": 0x2F, ".": 0x40, ",": 0x41, "'": 0x42,
		"[": 0x43, "]": 0x44, "\\": 0x45, ";": 0x46,
		"=": 0x47, "-": 0x48,
		"leftcontrol": 0x50, "rightcontrol": 0x51,
		"leftshift": 0x52, "rightshift": 0x53,
		"leftalt": 0x54, "rightalt": 0x55,
		"capslock": 0x56, "esc": 0x57,
		"tab": 0x58, "space": 0x59,
	}

	if isLinux() {
		code, exists := linuxMapping[key]
		return code, exists
	}
	code, exists := vkMapping[key]
	return code, exists
}

func isLinux() bool {
	return strings.Contains(strings.ToLower(runtime.GOOS), "linux")
}

func registerHotkey() {
	stateMu.RLock()
	if !state.IsActive {
		stateMu.RUnlock()
		return
	}
	stateMu.RUnlock()

	hookMu.Lock()
	defer hookMu.Unlock()

	log.Println("Регистрация хоткея: старт")
	if stopHookCancel != nil {
		log.Println("Остановка предыдущего хука...")
		stopHookCancel()
		hookWG.Wait()
		stopHookCtx = nil
		stopHookCancel = nil
	}

	stopHookCtx, stopHookCancel = context.WithCancel(context.Background())
	hookEvents = hook.Start()
	if hookEvents == nil {
		log.Println("Ошибка: не удалось инициализировать хук")
		return
	}

	stateMu.RLock()
	currentHotkey := state.Hotkey
	stateMu.RUnlock()

	targetCode, ok := getVKFromKey(currentHotkey)
	if !ok {
		log.Printf("Неизвестная горячая клавиша: %s", currentHotkey)
		return
	}

	log.Printf("Регистрация хоткея: %s", currentHotkey)
	hookWG.Add(1)
	go func() {
		defer hookWG.Done()
		var endOnce sync.Once
		safeEndHook := func() {
			endOnce.Do(func() {
				defer func() {
					_ = recover()
				}()
				hook.End()
				log.Println("Хук остановлен")
			})
		}

		log.Printf("Хук активен для: %s", currentHotkey)
		for {
			select {
			case <-stopHookCtx.Done():
				safeEndHook()
				return
			case ev, ok := <-hookEvents:
				if !ok {
					log.Println("Канал хука закрыт")
					return
				}
				log.Printf("Получено событие: Kind=%v, Rawcode=%v", ev.Kind, ev.Rawcode)
				if ev.Kind == 4 && ev.Rawcode == targetCode {
					log.Printf("Нажата клавиша: %s (Rawcode: %v)", currentHotkey, ev.Rawcode)
					handleHotkeyPress()
				}
			}
		}
	}()
}

func handleHotkeyPress() {
	stateMu.RLock()
	defer stateMu.RUnlock()
	if state.Conn == nil || !state.IsActive {
		log.Println("Соединение неактивно или отключено")
		return
	}
	x, y := robotgo.GetMousePos()
	screenW, screenH := robotgo.GetScreenSize()
	msg := map[string]interface{}{
		"type": "sync_click",
		"relX": float64(x) / float64(screenW),
		"relY": float64(y) / float64(screenH),
	}
	if err := state.Conn.WriteJSON(msg); err != nil {
		log.Printf("Ошибка отправки: %v", err)
		reconnect()
		return
	}
	go performClick(x, y)
}

func performClick(x, y int) {
	go func() {
		robotgo.Move(x, y)
		robotgo.MilliSleep(50)
		robotgo.Click("left")
		log.Printf("Клик выполнен на %d,%d", x, y)
	}()
}

func connectToServer() {
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		log.Printf("Ошибка подключения: %v", err)
		return
	}

	stateMu.Lock()
	state.Conn = conn
	state.IsActive = true
	stateMu.Unlock()

	log.Println("Успешное подключение к серверу")
	fyne.Do(func() {
		showMainUI()
		registerHotkey()
	})
	netWG.Add(1)
	go func() {
		defer netWG.Done()
		readMessages()
	}()
	netWG.Add(1)
	go func() {
		defer netWG.Done()
		keepAlive()
	}()
}

func showMainUI() {
	hotkeyEntry := widget.NewEntry()
	hotkeyEntry.SetText(state.Hotkey)

	// При нажатии на кнопку "Захватить клавишу" сразу захватывается и сохраняется новый хоткей
	captureButton := widget.NewButton("Захватить клавишу", func() {
		mainWindow.Canvas().SetOnTypedKey(func(e *fyne.KeyEvent) {
			// Сохраняем нажатую клавишу, как строку
			keyStr := strings.ToLower(string(e.Name))
			hotkeyEntry.SetText(keyStr)

			stateMu.Lock()
			state.Hotkey = keyStr
			stateMu.Unlock()
			log.Printf("Новая горячая клавиша установлена: %s", keyStr)
			go registerHotkey()

			mainWindow.Canvas().SetOnTypedKey(nil)
		})
	})

	// Убираем кнопку "Сохранить" – изменение происходит сразу при захвате
	content := container.NewVBox(
		container.NewHBox(
			widget.NewIcon(theme.ConfirmIcon()),
			widget.NewLabel("Статус: Активно"),
		),
		widget.NewSeparator(),
		widget.NewLabel("Горячая клавиша:"),
		hotkeyEntry,
		captureButton,
	)
	fyne.Do(func() {
		mainWindow.SetContent(content)
		mainWindow.Show()
	})
}

func readMessages() {
	defer netWG.Done()
	defer reconnect()
	for {
		stateMu.RLock()
		conn := state.Conn
		stateMu.RUnlock()
		if conn == nil {
			return
		}
		_, msg, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Ошибка чтения: %v", err)
			return
		}
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

func handleRemoteClick(relX, relY float64) {
	log.Printf("Получен удаленный клик: X=%.2f, Y=%.2f", relX, relY)
	screenW, screenH := robotgo.GetScreenSize()
	x := int(relX * float64(screenW))
	y := int(relY * float64(screenH))
	go func() {
		robotgo.MoveSmooth(x, y, 0.5, 0.5)
		robotgo.MilliSleep(50)
		robotgo.Click("left")
		log.Printf("Клик выполнен на %d,%d", x, y)
	}()
}

func keepAlive() {
	defer netWG.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		stateMu.RLock()
		conn := state.Conn
		stateMu.RUnlock()
		if conn == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := conn.WriteControl(
			websocket.PingMessage,
			[]byte{},
			time.Now().Add(1*time.Second),
		)
		if err != nil {
			log.Printf("Ошибка ping: %v", err)
			reconnect()
			return
		}
		select {
		case <-ctx.Done():
			log.Println("Таймаут подключения")
			reconnect()
			return
		default:
		}
	}
}

func reconnect() {
	stateMu.Lock()
	defer stateMu.Unlock()
	if state.IsActive {
		log.Println("Инициируем переподключение...")
		state.IsActive = false
		if state.Conn != nil {
			state.Conn.Close()
			state.Conn = nil
		}
		go func() {
			time.Sleep(3 * time.Second)
			log.Println("Попытка переподключения...")
			connectToServer()
		}()
	}
}

func saveHotkey(newHotkey string) bool {
	validKeys := map[string]bool{
		"f1": true, "f2": true, "f3": true, "f4": true, "f5": true,
		"f6": true, "f7": true, "f8": true, "f9": true, "f10": true,
		"f11": true, "f12": true,
		"a": true, "b": true, "c": true, "d": true, "e": true, "f": true,
		"g": true, "h": true, "i": true, "j": true, "k": true, "l": true,
		"m": true, "n": true, "o": true, "p": true, "q": true, "r": true,
		"s": true, "t": true, "u": true, "v": true, "w": true, "x": true,
		"y": true, "z": true,
		"0": true, "1": true, "2": true, "3": true, "4": true,
		"5": true, "6": true, "7": true, "8": true, "9": true,
		"/": true, ".": true, ",": true, "'": true, "[": true, "]": true,
		"\\": true, ";": true, "=": true, "-": true,
		"leftcontrol": true, "rightcontrol": true,
		"leftshift": true, "rightshift": true,
		"leftalt": true, "rightalt": true,
		"capslock": true, "esc": true, "tab": true, "space": true,
	}
	return validKeys[newHotkey]
}
