package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"scanner-kiosk/internal/buildinfo"
	"syscall"

	"scanner-kiosk/internal/apiruntime"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/pdfassemble"
	"scanner-kiosk/internal/pdfimport"
	"scanner-kiosk/internal/renderjob"
	"scanner-kiosk/internal/staterecovery"
	"scanner-kiosk/internal/useradmin"
	"scanner-kiosk/internal/workerruntime"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "__pdf-raster" {
		os.Exit(renderjob.PDFRasterChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "__pdf-assemble" {
		os.Exit(pdfassemble.AssemblyChild(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == "__pdf-import" {
		os.Exit(pdfimport.Child(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) == 2 && os.Args[1] == "version" {
		if err := json.NewEncoder(os.Stdout).Encode(buildinfo.Current()); err != nil {
			os.Exit(1)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if len(os.Args) >= 2 && os.Args[1] == "state" {
		if err := staterecovery.Run(ctx, os.Args[2:], os.LookupEnv, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "user" {
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, useradmin.ErrUsage)
			os.Exit(2)
		}
		if err := useradmin.Run(ctx, os.Args[2:], os.LookupEnv, os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) >= 3 && os.Args[1] == "worker" && os.Args[2] == "recover" {
		if len(os.Args) != 4 || os.Args[3] != "--previous-runtime-stopped" {
			fmt.Fprintln(os.Stderr, "usage: scanner worker recover --previous-runtime-stopped (terminate the previous runtime and all child processes first)")
			os.Exit(2)
		}
		c, err := config.LoadWorker(os.LookupEnv)
		if err == nil {
			err = workerruntime.Recover(ctx, c, true)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout, "Interrupted work recovered. Existing unexpired documents retained. Start worker normally; interrupted scans were not repeated.")
		return
	}

	valid := len(os.Args) == 2 && os.Args[1] == "serve" || len(os.Args) >= 2 && len(os.Args) <= 3 && os.Args[1] == "worker" && (len(os.Args) == 2 || os.Args[2] == "--once")
	if !valid {
		fmt.Fprintln(os.Stderr, "usage: scanner version | scanner serve | scanner worker [--once] | scanner worker recover --previous-runtime-stopped | scanner user create|set-password USERNAME | scanner state export|restore PRIVATE_ARCHIVE_PATH")
		os.Exit(2)
	}
	var c config.Config
	var err error
	if os.Args[1] == "serve" {
		c, err = config.Load(os.LookupEnv)
	} else {
		c, err = config.LoadWorker(os.LookupEnv)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if os.Args[1] == "serve" {
		err = apiruntime.Run(ctx, c)
	} else {
		err = workerruntime.Run(ctx, c, len(os.Args) == 3)
	}
	if err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
