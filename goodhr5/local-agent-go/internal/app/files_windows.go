//go:build windows

// Package app 提供 Windows 原生下载提示窗能力。
package app

import (
	"log"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsToastClassName = "GoodHRDownloadToastWindow"

	windowsWSOverlapped    = 0x00000000
	windowsWSCaption       = 0x00C00000
	windowsWSSysMenu       = 0x00080000
	windowsWSVisible       = 0x10000000
	windowsWSChild         = 0x40000000
	windowsWSBorder        = 0x00800000
	windowsBSDefPushButton = 0x00000001
	windowsSSLeft          = 0x00000000
	windowsCWUseDefault    = 0x80000000
	windowsSWShow          = 5
	windowsSWPNoSize       = 0x0001
	windowsSWPNoMove       = 0x0002
	windowsHWNDTopMost     = ^uintptr(0)
	windowsWMCreate        = 0x0001
	windowsWMDestroy       = 0x0002
	windowsWMCommand       = 0x0111
	windowsWMClose         = 0x0010
	windowsWMTimer         = 0x0113
	windowsColorWindow     = 5
	windowsIDOpen          = 1001
	windowsIDReveal        = 1002
	windowsIDDismiss       = 1003
	windowsIDTimer         = 2001
	windowsToastWidth      = 420
	windowsToastHeight     = 160
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procKillTimer        = user32.NewProc("KillTimer")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procSetTimer         = user32.NewProc("SetTimer")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procShowWindow       = user32.NewProc("ShowWindow")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetStockObject   = gdi32.NewProc("GetStockObject")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")

	registerToastClassOnce sync.Once
	registerToastClassErr  error
	windowsToastMu         sync.Mutex
	currentToast           *windowsToastState
)

type windowsToastState struct {
	action string
	done   chan string
}

type windowsWndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type windowsMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct {
		x int32
		y int32
	}
}

// showDownloadToastWindowsNative 使用 Windows API 弹出下载提示窗。
// filePath 为下载文件路径，返回用户动作。
func showDownloadToastWindowsNative(filePath string) (string, error) {
	windowsToastMu.Lock()
	defer windowsToastMu.Unlock()
	if err := registerWindowsToastClass(); err != nil {
		log.Printf("[下载提示] Windows 原生提示窗注册失败 err=%v", err)
		return "", err
	}

	state := &windowsToastState{action: "dismiss", done: make(chan string, 1)}
	currentToast = state
	title := syscall.StringToUTF16Ptr("GoodHR")
	className := syscall.StringToUTF16Ptr(windowsToastClassName)
	screenW, _, _ := procGetSystemMetrics.Call(0)
	screenH, _, _ := procGetSystemMetrics.Call(1)
	x := int(screenW) - windowsToastWidth - 24
	y := int(screenH) - windowsToastHeight - 72
	if x < 0 {
		x = int(windowsCWUseDefault)
	}
	if y < 0 {
		y = int(windowsCWUseDefault)
	}

	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		uintptr(windowsWSOverlapped|windowsWSCaption|windowsWSSysMenu|windowsWSBorder|windowsWSVisible),
		uintptr(x),
		uintptr(y),
		windowsToastWidth,
		windowsToastHeight,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(filepath.Base(filePath)))),
	)
	if hwnd == 0 {
		log.Printf("[下载提示] Windows 原生提示窗创建失败 err=%v", err)
		return "", err
	}

	procSetWindowPos.Call(hwnd, windowsHWNDTopMost, 0, 0, 0, 0, windowsSWPNoMove|windowsSWPNoSize)
	procShowWindow.Call(hwnd, windowsSWShow)
	procUpdateWindow.Call(hwnd)
	procSetTimer.Call(hwnd, windowsIDTimer, uintptr(downloadToastTimeoutSeconds*1000), 0)

	var msg windowsMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	action := state.action
	select {
	case action = <-state.done:
	default:
	}
	currentToast = nil
	return action, nil
}

// registerWindowsToastClass 注册 Windows 提示窗窗口类。
func registerWindowsToastClass() error {
	registerToastClassOnce.Do(func() {
		className := syscall.StringToUTF16Ptr(windowsToastClassName)
		instance, _, callErr := procGetModuleHandleW.Call(0)
		if instance == 0 {
			registerToastClassErr = callErr
			return
		}
		background, _, _ := procGetStockObject.Call(windowsColorWindow)
		class := windowsWndClassEx{
			size:       uint32(unsafe.Sizeof(windowsWndClassEx{})),
			wndProc:    windows.NewCallback(windowsToastWndProc),
			instance:   windows.Handle(instance),
			background: windows.Handle(background),
			className:  className,
		}
		ret, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class)))
		if ret == 0 {
			registerToastClassErr = callErr
		}
	})
	return registerToastClassErr
}

// windowsToastWndProc 处理 Windows 原生提示窗消息。
func windowsToastWndProc(hwnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case windowsWMCreate:
		fileName := "下载文件"
		if lParam != 0 {
			createStruct := (*struct {
				createParams uintptr
				instance     uintptr
				menu         uintptr
				parent       uintptr
				cy           int32
				cx           int32
				y            int32
				x            int32
				style        int32
				name         uintptr
				class        uintptr
				exStyle      uint32
			})(unsafe.Pointer(lParam))
			if createStruct.createParams != 0 {
				fileName = windows.UTF16PtrToString((*uint16)(unsafe.Pointer(createStruct.createParams)))
			}
		}
		createToastLabel(hwnd, "下载好了，公主请验收", 18, 16, 370, 24)
		createToastLabel(hwnd, fileName, 18, 46, 370, 34)
		createToastButton(hwnd, "先放着", windowsIDDismiss, 36, 98, 82, 30)
		createToastButton(hwnd, "打开文件", windowsIDOpen, 128, 98, 88, 30)
		createToastButton(hwnd, "打开文件夹", windowsIDReveal, 226, 98, 104, 30)
	case windowsWMCommand:
		switch uint16(wParam & 0xffff) {
		case windowsIDOpen:
			finishWindowsToast(hwnd, "open")
		case windowsIDReveal:
			finishWindowsToast(hwnd, "reveal")
		case windowsIDDismiss:
			finishWindowsToast(hwnd, "dismiss")
		}
	case windowsWMTimer:
		finishWindowsToast(hwnd, "timeout")
	case windowsWMClose:
		finishWindowsToast(hwnd, "dismiss")
	case windowsWMDestroy:
		procPostQuitMessage.Call(0)
	default:
		ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
		return ret
	}
	return 0
}

// createToastLabel 创建 Windows 提示窗文本。
func createToastLabel(parent uintptr, text string, x int, y int, width int, height int) {
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("STATIC"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(windowsWSChild|windowsWSVisible|windowsSSLeft),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		parent, 0, 0, 0,
	)
}

// createToastButton 创建 Windows 提示窗按钮。
func createToastButton(parent uintptr, text string, id uintptr, x int, y int, width int, height int) {
	procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))),
		uintptr(windowsWSChild|windowsWSVisible|windowsBSDefPushButton),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		parent, id, 0, 0,
	)
}

// finishWindowsToast 结束 Windows 提示窗并记录用户动作。
func finishWindowsToast(hwnd uintptr, action string) {
	if currentToast != nil {
		currentToast.action = action
		select {
		case currentToast.done <- action:
		default:
		}
	}
	procKillTimer.Call(hwnd, windowsIDTimer)
	procDestroyWindow.Call(hwnd)
}
