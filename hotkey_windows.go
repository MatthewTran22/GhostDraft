package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procGetAsyncKeyState    = user32.NewProc("GetAsyncKeyState")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowLongW      = user32.NewProc("GetWindowLongW")
	procSetWindowLongW      = user32.NewProc("SetWindowLongW")
	procFindWindowW         = user32.NewProc("FindWindowW")
)

const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	VK_O           = 0x4F
	VK_CONTROL     = 0x11
	VK_TAB         = 0x09

	// Window style constants for click-through
	WS_EX_TRANSPARENT = 0x00000020
	WS_EX_LAYERED     = 0x00080000
)

// KBDLLHOOKSTRUCT contains information about a low-level keyboard input event
type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type MSG struct {
	HWND    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

var appInstance *App
var keyboardHook uintptr
var tabPressed bool
var stopGoldPoll chan struct{}

// Track if gold box is shown during in-game Tab hold
var isGoldBoxVisible bool

// Saved window state for restoring after gold box
var savedWindowX, savedWindowY, savedWindowW, savedWindowH int

// isKeyPressed checks if a key is currently pressed
func isKeyPressed(vk uintptr) bool {
	ret, _, _ := procGetAsyncKeyState.Call(vk)
	return ret&0x8000 != 0
}

// findGhostDraftWindow finds the GhostDraft window handle
func findGhostDraftWindow() uintptr {
	title, _ := syscall.UTF16PtrFromString("GhostDraft")
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	return hwnd
}

// setWindowClickThrough makes a window click-through (mouse events pass through)
func setWindowClickThrough(hwnd uintptr, clickThrough bool) {
	if hwnd == 0 {
		return
	}

	// GWL_EXSTYLE is -20, need to convert to uintptr properly
	gwlExStyle := uintptr(0xFFFFFFEC) // -20 as unsigned

	// Get current extended style
	exStyle, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)

	if clickThrough {
		// Add WS_EX_TRANSPARENT and WS_EX_LAYERED to make click-through
		newStyle := exStyle | WS_EX_TRANSPARENT | WS_EX_LAYERED
		procSetWindowLongW.Call(hwnd, gwlExStyle, newStyle)
	} else {
		// Remove WS_EX_TRANSPARENT to restore normal behavior
		newStyle := exStyle &^ WS_EX_TRANSPARENT
		procSetWindowLongW.Call(hwnd, gwlExStyle, newStyle)
	}
}

// keyboardProc is the low-level keyboard hook callback
func keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		kbStruct := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))

		if wParam == WM_KEYDOWN {
			// Check for Ctrl+O
			if kbStruct.VkCode == VK_O && isKeyPressed(VK_CONTROL) {
				if appInstance != nil {
					appInstance.ToggleWindow()
				}
			}
			// Check for Tab press
			if kbStruct.VkCode == VK_TAB && !tabPressed {
				tabPressed = true
				if appInstance != nil {
					appInstance.onTabPressed()
				}
			}
		} else if wParam == WM_KEYUP {
			// Check for Tab release
			if kbStruct.VkCode == VK_TAB && tabPressed {
				tabPressed = false
				if appInstance != nil {
					appInstance.onTabReleased()
				}
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(keyboardHook, uintptr(nCode), wParam, lParam)
	return ret
}

// RegisterToggleHotkey registers Ctrl+O as a global hotkey using low-level keyboard hook
func (a *App) RegisterToggleHotkey() {
	appInstance = a

	go func() {
		// Create callback
		callback := syscall.NewCallback(keyboardProc)

		// Install the low-level keyboard hook
		ret, _, err := procSetWindowsHookEx.Call(
			WH_KEYBOARD_LL,
			callback,
			0,
			0,
		)
		if ret == 0 {
			fmt.Printf("Failed to install keyboard hook: %v\n", err)
			return
		}
		keyboardHook = ret
		fmt.Println("Installed low-level keyboard hook for Ctrl+O")

		// Message loop to keep the hook alive
		var msg MSG
		for {
			ret, _, _ := procGetMessage.Call(
				uintptr(unsafe.Pointer(&msg)),
				0, 0, 0,
			)
			if ret == 0 {
				break
			}
		}
	}()
}

// ToggleWindow toggles the window visibility
func (a *App) ToggleWindow() {
	a.windowVisible = !a.windowVisible
	if a.windowVisible {
		fmt.Println("Showing overlay (Ctrl+O)")
		a.showWindow()
	} else {
		fmt.Println("Hiding overlay (Ctrl+O)")
		a.hideWindow()
	}
}

func (a *App) showWindow() {
	if a.ctx != nil {
		// Use wails runtime to show
		go func() {
			// Small delay to ensure we're not in a Windows message loop
			if a.ctx != nil {
				wailsRuntime.WindowShow(a.ctx)
			}
		}()
	}
}

func (a *App) hideWindow() {
	if a.ctx != nil {
		go func() {
			if a.ctx != nil {
				wailsRuntime.WindowHide(a.ctx)
			}
		}()
	}
}

// onTabPressed shows gold box overlay when Tab is held in-game
func (a *App) onTabPressed() {
	// Only activate if in game
	if !a.liveClient.IsGameRunning() {
		return
	}

	isGoldBoxVisible = true

	// Save current window state
	savedWindowX, savedWindowY = wailsRuntime.WindowGetPosition(a.ctx)
	savedWindowW, savedWindowH = wailsRuntime.WindowGetSize(a.ctx)

	// Tell frontend to show gold box mode FIRST (hides overlay-box)
	wailsRuntime.EventsEmit(a.ctx, "goldbox:show", true)

	// Small delay to let frontend hide the overlay-box before resize
	time.Sleep(10 * time.Millisecond)

	// Get screen dimensions
	screens, err := wailsRuntime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return
	}
	screen := screens[0]

	// Fullscreen transparent overlay
	wailsRuntime.WindowSetSize(a.ctx, screen.Size.Width, screen.Size.Height)
	wailsRuntime.WindowSetPosition(a.ctx, 0, 0)
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)

	a.showWindow()

	// Make window click-through so it doesn't capture mouse input
	// Small delay to ensure window is shown before modifying style
	time.Sleep(20 * time.Millisecond)
	hwnd := findGhostDraftWindow()
	setWindowClickThrough(hwnd, true)

	// Start polling gold data
	if stopGoldPoll != nil {
		close(stopGoldPoll)
	}
	stopGoldPoll = make(chan struct{})

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		// Emit immediately
		a.emitGoldUpdate()

		for {
			select {
			case <-stopGoldPoll:
				return
			case <-ticker.C:
				a.emitGoldUpdate()
			}
		}
	}()
}

// onTabReleased hides gold box overlay
func (a *App) onTabReleased() {
	// Stop gold polling
	if stopGoldPoll != nil {
		close(stopGoldPoll)
		stopGoldPoll = nil
	}

	// Hide and restore window if gold box was showing
	if isGoldBoxVisible {
		isGoldBoxVisible = false

		// Tell frontend to hide gold box mode
		wailsRuntime.EventsEmit(a.ctx, "goldbox:show", false)

		// Restore mouse events (no longer click-through)
		hwnd := findGhostDraftWindow()
		setWindowClickThrough(hwnd, false)

		// Hide window
		a.hideWindow()
		wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)

		// Restore window size/position
		wailsRuntime.WindowSetSize(a.ctx, savedWindowW, savedWindowH)
		wailsRuntime.WindowSetPosition(a.ctx, savedWindowX, savedWindowY)
	}
}

// emitGoldUpdate fetches and emits gold data to frontend
func (a *App) emitGoldUpdate() {
	if a.ctx == nil {
		return
	}
	data := a.GetGoldDiff()
	wailsRuntime.EventsEmit(a.ctx, "gold:update", data)
}

// HideForGame hides the overlay when entering a game
func (a *App) HideForGame() {
	if a.ctx == nil {
		return
	}
	a.hideWindow()
}

// ShowAfterGame shows the overlay when leaving a game
func (a *App) ShowAfterGame() {
	if a.ctx == nil {
		return
	}
	a.showWindow()
}
