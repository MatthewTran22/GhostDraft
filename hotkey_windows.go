package main

import (
	"fmt"
	"syscall"
	"unsafe"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey  = user32.NewProc("RegisterHotKey")
	procGetMessage      = user32.NewProc("GetMessageW")
)

const (
	MOD_CONTROL = 0x0002
	MOD_NOREPEAT = 0x4000
	VK_O        = 0x4F
	WM_HOTKEY   = 0x0312
)

type MSG struct {
	HWND    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// RegisterToggleHotkey registers Ctrl+O as a global hotkey
func (a *App) RegisterToggleHotkey() {
	go func() {
		// Register Ctrl+O (id = 1)
		ret, _, err := procRegisterHotKey.Call(
			0,                          // hwnd (0 = current thread)
			1,                          // id
			uintptr(MOD_CONTROL|MOD_NOREPEAT), // modifiers
			uintptr(VK_O),              // virtual key code
		)
		if ret == 0 {
			fmt.Printf("Failed to register hotkey: %v\n", err)
			return
		}
		fmt.Println("Registered Ctrl+O hotkey to toggle overlay")

		// Message loop to receive hotkey events
		var msg MSG
		for {
			ret, _, _ := procGetMessage.Call(
				uintptr(unsafe.Pointer(&msg)),
				0, 0, 0,
			)
			if ret == 0 {
				break
			}

			if msg.Message == WM_HOTKEY {
				a.ToggleWindow()
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
