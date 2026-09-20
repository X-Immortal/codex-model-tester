//go:build !windows && !darwin

package main

func showFatalError(_ string, _ error) {}
