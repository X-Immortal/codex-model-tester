//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var messageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func showFatalError(title string, err error) {
	titleUTF16, titleErr := windows.UTF16PtrFromString(title)
	messageUTF16, messageErr := windows.UTF16PtrFromString(fmt.Sprint(err))
	if titleErr != nil || messageErr != nil {
		return
	}
	const mbIconError = 0x00000010
	_, _, _ = messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messageUTF16)),
		uintptr(unsafe.Pointer(titleUTF16)),
		mbIconError,
	)
}
