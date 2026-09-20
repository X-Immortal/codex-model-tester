//go:build darwin

// Package macosdock provides the small AppKit bridge needed by the menu-bar
// application. The systray library intentionally uses accessory activation
// policy, so this bridge restores a regular Dock presence and handles Dock
// reopen events without introducing a second UI toolkit.
package macosdock

import (
	"errors"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/go-webgpu/goffi/ffi"
	"github.com/go-webgpu/goffi/types"
)

type runtimeState struct {
	once sync.Once
	err  error

	objc            unsafe.Pointer
	objcGetClass    unsafe.Pointer
	selRegisterName unsafe.Pointer
	objcMsgSend     unsafe.Pointer
	allocateClass   unsafe.Pointer
	addMethod       unsafe.Pointer
	registerClass   unsafe.Pointer
}

var rt runtimeState

var (
	delegateClass  uintptr
	delegate       uintptr
	app            uintptr
	regularSel     uintptr
	reopenSel      uintptr
	showRegularIMP uintptr
	reopenIMP      uintptr
)

var reopenHandler func()

func initRuntime() error {
	rt.once.Do(func() {
		rt.objc, rt.err = ffi.LoadLibrary("/usr/lib/libobjc.A.dylib")
		if rt.err != nil {
			return
		}
		if rt.objcGetClass, rt.err = ffi.GetSymbol(rt.objc, "objc_getClass"); rt.err != nil {
			return
		}
		if rt.selRegisterName, rt.err = ffi.GetSymbol(rt.objc, "sel_registerName"); rt.err != nil {
			return
		}
		if rt.objcMsgSend, rt.err = ffi.GetSymbol(rt.objc, "objc_msgSend"); rt.err != nil {
			return
		}
		if rt.allocateClass, rt.err = ffi.GetSymbol(rt.objc, "objc_allocateClassPair"); rt.err != nil {
			return
		}
		if rt.addMethod, rt.err = ffi.GetSymbol(rt.objc, "class_addMethod"); rt.err != nil {
			return
		}
		rt.registerClass, rt.err = ffi.GetSymbol(rt.objc, "objc_registerClassPair")
	})
	return rt.err
}

func call(fn unsafe.Pointer, returnType *types.TypeDescriptor, argTypes []*types.TypeDescriptor, args ...uintptr) (uintptr, error) {
	cif := &types.CallInterface{}
	if err := ffi.PrepareCallInterface(cif, types.DefaultCall, returnType, argTypes); err != nil {
		return 0, err
	}
	values := make([]uintptr, len(args))
	argPtrs := make([]unsafe.Pointer, len(args))
	for i, value := range args {
		values[i] = value
		argPtrs[i] = unsafe.Pointer(&values[i])
	}
	var result uintptr
	if _, err := ffi.CallFunction(cif, fn, unsafe.Pointer(&result), argPtrs); err != nil {
		return 0, err
	}
	return result, nil
}

func pointerCall(fn unsafe.Pointer, args ...uintptr) (uintptr, error) {
	typesList := make([]*types.TypeDescriptor, len(args))
	for i := range typesList {
		typesList[i] = types.PointerTypeDescriptor
	}
	return call(fn, types.PointerTypeDescriptor, typesList, args...)
}

func voidCall(fn unsafe.Pointer, argTypes []*types.TypeDescriptor, args ...uintptr) error {
	_, err := call(fn, types.VoidTypeDescriptor, argTypes, args...)
	return err
}

func class(name string) (uintptr, error) {
	if err := initRuntime(); err != nil {
		return 0, err
	}
	value := append([]byte(name), 0)
	result, err := pointerCall(rt.objcGetClass, uintptr(unsafe.Pointer(&value[0])))
	runtime.KeepAlive(value)
	return result, err
}

func selector(name string) (uintptr, error) {
	if err := initRuntime(); err != nil {
		return 0, err
	}
	value := append([]byte(name), 0)
	result, err := pointerCall(rt.selRegisterName, uintptr(unsafe.Pointer(&value[0])))
	runtime.KeepAlive(value)
	return result, err
}

func message(self, sel uintptr, args ...uintptr) (uintptr, error) {
	all := make([]uintptr, 2, 2+len(args))
	all[0], all[1] = self, sel
	all = append(all, args...)
	return pointerCall(rt.objcMsgSend, all...)
}

func messageWithInt(self, sel uintptr, value int64) error {
	argTypes := []*types.TypeDescriptor{
		types.PointerTypeDescriptor,
		types.PointerTypeDescriptor,
		types.SInt64TypeDescriptor,
	}
	_, err := call(rt.objcMsgSend, types.PointerTypeDescriptor, argTypes, self, sel, uintptr(value))
	return err
}

func registerDelegate() error {
	if delegateClass != 0 {
		return nil
	}
	nsObject, err := class("NSObject")
	if err != nil || nsObject == 0 {
		return errors.New("load NSObject class")
	}
	className := append([]byte("CodexDockDelegate"), 0)
	delegateClass, err = pointerCall(rt.allocateClass, nsObject, uintptr(unsafe.Pointer(&className[0])), 0)
	runtime.KeepAlive(className)
	if err != nil || delegateClass == 0 {
		return errors.New("allocate Dock delegate class")
	}

	showRegularIMP = ffi.NewCallback(func(_, _ uintptr, _ uintptr) uintptr {
		if app != 0 && regularSel != 0 {
			_ = messageWithInt(app, regularSel, 0) // NSApplicationActivationPolicyRegular
		}
		return 0
	})
	showSelector, err := selector("codexShowRegular:")
	if err != nil {
		return err
	}
	showTypes := append([]byte("v@:@"), 0)
	if _, err = pointerCall(rt.addMethod, delegateClass, showSelector, showRegularIMP, uintptr(unsafe.Pointer(&showTypes[0]))); err != nil {
		return err
	}
	runtime.KeepAlive(showTypes)

	reopenIMP = ffi.NewCallback(func(_, _ uintptr, _, _ uintptr) uintptr {
		if fn := reopenHandler; fn != nil {
			go fn()
		}
		return 1
	})
	reopenTypes := append([]byte("B@:@B"), 0)
	if _, err = pointerCall(rt.addMethod, delegateClass, reopenSel, reopenIMP, uintptr(unsafe.Pointer(&reopenTypes[0]))); err != nil {
		return err
	}
	runtime.KeepAlive(reopenTypes)
	if err = voidCall(rt.registerClass, []*types.TypeDescriptor{types.PointerTypeDescriptor}, delegateClass); err != nil {
		return err
	}
	return nil
}

// Setup makes the running process appear in the Dock and opens the Web UI
// when macOS sends the normal application-reopen event for its Dock icon.
// It returns a cleanup function for symmetry with the application lifecycle.
func Setup(onReopen func()) func() {
	if initRuntime() != nil {
		return func() {}
	}
	var err error
	appClass, err := class("NSApplication")
	if err != nil {
		return func() {}
	}
	shared, err := selector("sharedApplication")
	if err != nil {
		return func() {}
	}
	app, err = message(appClass, shared)
	if err != nil || app == 0 {
		return func() {}
	}
	regularSel, err = selector("setActivationPolicy:")
	if err != nil {
		return func() {}
	}
	reopenSel, err = selector("applicationShouldHandleReopen:hasVisibleWindows:")
	if err != nil {
		return func() {}
	}
	if err := registerDelegate(); err != nil {
		return func() {}
	}

	alloc, _ := selector("alloc")
	initSel, _ := selector("init")
	delegate, _ = message(delegateClass, alloc)
	delegate, _ = message(delegate, initSel)
	setDelegate, _ := selector("setDelegate:")
	_, _ = message(app, setDelegate, delegate)
	reopenHandler = onReopen

	// systray sets accessory policy inside Run(). Queue our regular policy after
	// the Cocoa event loop starts so the Dock icon wins deterministically.
	perform, _ := selector("performSelectorOnMainThread:withObject:waitUntilDone:")
	showRegular, _ := selector("codexShowRegular:")
	go func() {
		time.Sleep(250 * time.Millisecond)
		_, _ = call(rt.objcMsgSend, types.PointerTypeDescriptor, []*types.TypeDescriptor{
			types.PointerTypeDescriptor,
			types.PointerTypeDescriptor,
			types.PointerTypeDescriptor,
			types.PointerTypeDescriptor,
			types.UInt8TypeDescriptor,
		}, delegate, perform, showRegular, 0, 0)
	}()

	return func() { reopenHandler = nil }
}
