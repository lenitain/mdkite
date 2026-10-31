// mdkite is a standalone host for the mdkite.nvim browser preview.
// It serves an embedded copy of the preview UI for one Markdown file.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var version = "0.1.0"

const upstreamVersion = "v2.1.0"

func main() {
	args, help := parseArgs(os.Args[1:])
	if help {
		printHelp(os.Stdout)
		return
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "mdkite: missing file")
		fmt.Fprintln(os.Stderr, "usage: mdkite <file.md>")
		fmt.Fprintln(os.Stderr, "try 'mdkite --help'")
		os.Exit(2)
	}
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "mdkite: expected one file, got %d\n", len(args))
		fmt.Fprintln(os.Stderr, "try 'mdkite --help'")
		os.Exit(2)
	}

	filePath, err := filepath.Abs(args[0])
	if err != nil {
		fatal("cannot resolve %s: %v", args[0], err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		fatal("cannot open %s: %v", args[0], err)
	}
	if !info.Mode().IsRegular() {
		fatal("%s is not a regular file", args[0])
	}
	file, err := os.Open(filePath)
	if err != nil {
		fatal("cannot read %s: %v", args[0], err)
	}
	file.Close()

	preview := newPreviewServer(filePath)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("cannot listen: %v", err)
	}
	url := "http://" + listener.Addr().String() + "/"

	fmt.Fprintf(os.Stderr, "mdkite %s\n", version)
	fmt.Fprintf(os.Stderr, "  file  %s\n", filePath)
	if preview.cdn.dir != "" {
		fmt.Fprintf(os.Stderr, "  cache %s\n", preview.cdn.dir)
	}
	fmt.Fprintf(os.Stderr, "  url   %s\n", url)
	fmt.Fprintln(os.Stderr, "  stop  Ctrl+C")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go preview.watch(ctx)
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- preview.httpServer.Serve(listener)
	}()

	if err := openBrowser(url); err != nil {
		fmt.Fprintf(os.Stderr, "mdkite: cannot open browser: %v\n", err)
		fmt.Fprintf(os.Stderr, "mdkite: open %s manually\n", url)
	}

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("%v", err)
		}
	case <-ctx.Done():
		stop() // restore default handling: a second Ctrl+C force-quits
		preview.stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := preview.httpServer.Shutdown(shutdownCtx); err != nil {
			_ = preview.httpServer.Close()
		}
		<-serveErr
	}
}

func parseArgs(argv []string) (args []string, help bool) {
	for _, arg := range argv {
		switch arg {
		case "-h", "--help", "-help":
			return nil, true
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			fmt.Fprintf(os.Stderr, "mdkite: unknown option %q\n", arg)
			fmt.Fprintln(os.Stderr, "try 'mdkite --help'")
			os.Exit(2)
		}
		args = append(args, arg)
	}
	return args, false
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `mdkite %s
Live Markdown preview in the browser, without Neovim.

Usage:
  mdkite <file.md>

Serves one file over a loopback HTTP server, opens it in your default
browser, and reloads it when the file changes. Mermaid diagrams, KaTeX
math, highlighted code, relative images and YAML front matter render as
they do in the mdkite.nvim preview. Front-end assets are downloaded on
first use and kept in the user cache directory.

Options:
  -h, --help    show this help

Preview UI: mdkite.nvim %s (MIT), Copyright (c) 2025 Selim Acerbas.
`, version, upstreamVersion)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mdkite: %s\n", fmt.Sprintf(format, args...))
	os.Exit(1)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
