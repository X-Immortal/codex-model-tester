//go:build !darwin && !windows

package config

func detectSystemProxy() string { return "" }
