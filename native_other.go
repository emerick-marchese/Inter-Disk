//go:build !windows

package main

import (
	"fmt"
	"os"
)

func showErrorBox(title, msg string) { fmt.Fprintf(os.Stderr, "%s : %s\n", title, msg) }

func beep() {}
