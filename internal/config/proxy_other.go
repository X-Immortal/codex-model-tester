//go:build !darwin

package config

func detectSystemProxy() string { return "" }
