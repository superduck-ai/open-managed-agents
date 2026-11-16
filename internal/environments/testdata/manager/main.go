package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var version string

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("environment-manager " + version)
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	<-signals
}
