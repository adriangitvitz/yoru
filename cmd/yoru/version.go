package main

import "fmt"

const Version = "0.1.0"

func versionCmd() int {
	fmt.Printf("yoru %s (Phase 0)\n", Version)
	return 0
}
