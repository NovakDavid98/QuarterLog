//go:build windows

package winutil

import (
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const appValueName = "Quarterlog"

// SingleInstance acquires a named mutex. It returns false if another instance
// already holds it. The handle is intentionally leaked for the process lifetime.
func SingleInstance(name string) bool {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return true
	}
	_, err = windows.CreateMutex(nil, false, p)
	// ERROR_ALREADY_EXISTS means another instance created it first.
	return err != windows.ERROR_ALREADY_EXISTS
}

// SessionLocked reports whether the interactive workstation is currently locked.
// It checks whether an open desktop handle to the input desktop can be obtained;
// on the secure/locked desktop this call fails with access denied.
func SessionLocked() bool {
	user32 := windows.NewLazySystemDLL("user32.dll")
	openInputDesktop := user32.NewProc("OpenInputDesktop")
	closeDesktop := user32.NewProc("CloseDesktop")

	const DESKTOP_READOBJECTS = 0x0001
	h, _, _ := openInputDesktop.Call(0, 0, DESKTOP_READOBJECTS)
	if h == 0 {
		return true // couldn't reach the input desktop => locked/secure desktop
	}
	closeDesktop.Call(h)
	return false
}

// WorkArea returns the primary monitor's work-area size in physical pixels
// (the screen minus the taskbar). Returns 0,0 on failure.
func WorkArea() (width, height int) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("SystemParametersInfoW")
	var r struct{ Left, Top, Right, Bottom int32 }
	const SPI_GETWORKAREA = 0x0030
	ret, _, _ := proc.Call(SPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&r)), 0)
	if ret == 0 {
		return 0, 0
	}
	return int(r.Right - r.Left), int(r.Bottom - r.Top)
}

// SetAutostart adds or removes the app from the per-user Run key.
func SetAutostart(enabled bool, exePath string) error {
	if !enabled {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			if err == registry.ErrNotExist {
				return nil
			}
			return err
		}
		defer k.Close()
		err = k.DeleteValue(appValueName)
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(appValueName, `"`+exePath+`"`)
}
