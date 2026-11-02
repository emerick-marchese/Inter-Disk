//go:build !linux && !darwin && !windows

package main

func freeSpace(path string) int64 { return -1 }
