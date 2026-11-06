//go:build linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"scanner-kiosk/internal/buildinfo"
	"scanner-kiosk/internal/ocr/tesseractapi"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		if json.NewEncoder(os.Stdout).Encode(buildinfo.Current()) != nil {
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: scanner-tesseract-api serve|version")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err := tesseractapi.Run(ctx, os.LookupEnv); err != nil {
		fmt.Fprintln(os.Stderr, "Tesseract OCR service unavailable or invalid")
		os.Exit(1)
	}
}
