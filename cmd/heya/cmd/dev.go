package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Start development servers",
	Long:  "Start the Go API server (with hot reload via air) and Nuxt dev server.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		rootDir, _ := os.Getwd()
		webDir := findWebDir()
		if webDir == "" {
			return fmt.Errorf("could not find web/ directory with package.json")
		}

		runtime := detectRuntime(webDir)

		if !hasNodeModules(webDir) {
			fmt.Printf("  \033[90m[dev]\033[0m Installing frontend dependencies with %s…\n", runtime)
			install := exec.CommandContext(ctx, runtime, "install")
			install.Dir = webDir
			install.Stdout = os.Stdout
			install.Stderr = os.Stderr
			if err := install.Run(); err != nil {
				return fmt.Errorf("failed to install frontend deps: %w", err)
			}
		}

		fmt.Println()
		fmt.Println("  \033[1;33m⚡ Heya Dev Mode\033[0m")
		fmt.Println()
		fmt.Println("  API server:  \033[36mhttp://localhost:8080\033[0m  (Go + air hot reload)")
		fmt.Println("  Frontend:    \033[36mhttp://localhost:3000\033[0m  ← open this")
		fmt.Println()
		fmt.Println("  Frontend proxies /api/* to the Go server.")
		fmt.Println("  Edit .go files → auto rebuild. Edit .vue files → instant HMR.")
		fmt.Println("  Press Ctrl+C to stop.")
		fmt.Println()

		var wg sync.WaitGroup

		// Start air (Go hot reload)
		air := exec.CommandContext(ctx, "go", "run", "github.com/air-verse/air@latest")
		air.Dir = rootDir
		air.Env = os.Environ()
		airOut, _ := air.StdoutPipe()
		airErr, _ := air.StderrPipe()
		if err := air.Start(); err != nil {
			return fmt.Errorf("failed to start air: %w (try: go install github.com/air-verse/air@latest)", err)
		}
		wg.Add(2)
		go func() { defer wg.Done(); pipeOutput(airOut, "\033[32mgo\033[0m") }()
		go func() { defer wg.Done(); pipeOutput(airErr, "\033[32mgo\033[0m") }()

		// Start Nuxt dev
		nuxt := exec.CommandContext(ctx, runtime, "run", "dev")
		nuxt.Dir = webDir
		nuxt.Env = os.Environ()
		nuxtOut, _ := nuxt.StdoutPipe()
		nuxtErr, _ := nuxt.StderrPipe()
		if err := nuxt.Start(); err != nil {
			return fmt.Errorf("failed to start nuxt dev: %w", err)
		}
		wg.Add(2)
		go func() { defer wg.Done(); pipeOutput(nuxtOut, "\033[35mnuxt\033[0m") }()
		go func() { defer wg.Done(); pipeOutput(nuxtErr, "\033[35mnuxt\033[0m") }()

		// Wait for signal
		<-ctx.Done()
		fmt.Println("\n  \033[90mShutting down dev servers…\033[0m")

		if air.Process != nil {
			air.Process.Signal(syscall.SIGTERM)
		}
		if nuxt.Process != nil {
			nuxt.Process.Signal(syscall.SIGTERM)
		}

		air.Wait()
		nuxt.Wait()
		wg.Wait()
		return nil
	},
}

func findWebDir() string {
	cwd, _ := os.Getwd()
	for _, c := range []string{"web", filepath.Join("..", "web")} {
		abs := filepath.Join(cwd, c)
		if info, err := os.Stat(filepath.Join(abs, "package.json")); err == nil && !info.IsDir() {
			return abs
		}
	}
	return ""
}

func detectRuntime(webDir string) string {
	if _, err := os.Stat(filepath.Join(webDir, "bun.lockb")); err == nil {
		if _, err := exec.LookPath("bun"); err == nil {
			return "bun"
		}
	}
	if _, err := exec.LookPath("bun"); err == nil {
		return "bun"
	}
	return "npm"
}

func hasNodeModules(webDir string) bool {
	info, err := os.Stat(filepath.Join(webDir, "node_modules"))
	return err == nil && info.IsDir()
}

func pipeOutput(r interface{ Read([]byte) (int, error) }, prefix string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fmt.Printf("  [%s] %s\n", prefix, scanner.Text())
	}
}

func init() {
	rootCmd.AddCommand(devCmd)
}
