//go:build !darwin

package macosdock

func Setup(func()) func() { return func() {} }
