package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/karbowiak/heya/internal/llm"
	"github.com/karbowiak/heya/internal/service"
	"github.com/spf13/cobra"
)

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "AI subsystem controls",
	Long:  "Configure, inspect, and exercise the LLM subsystem (local llama-server or an external OpenAI-compatible provider).",
}

var aiStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show AI mode, readiness, and local runtime state",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			st := app.AIStatus(ctx)
			fmt.Printf("mode          : %s\n", st.Mode)
			fmt.Printf("ready         : %v\n", st.Ready)
			if st.Detail != "" {
				fmt.Printf("detail        : %s\n", st.Detail)
			}
			if st.Mode == "external" {
				fmt.Printf("provider      : %s\n", st.Provider)
				fmt.Printf("model         : %s\n", st.Model)
			}
			fmt.Printf("local model   : %s\n", st.LocalModel)
			fmt.Printf("context size  : %d\n", st.ContextSize)
			fmt.Printf("llama.cpp     : %s (server present: %v, model present: %v)\n",
				st.Local.Build, st.Local.ServerPresent, st.Local.ModelPresent)
			fmt.Printf("running       : %v", st.Local.Running)
			if st.Local.RunningModel != "" {
				fmt.Printf(" (%s)", st.Local.RunningModel)
			}
			fmt.Println()
			if st.Local.DownloadState != string(llm.DownloadIdle) {
				fmt.Printf("download      : %s", st.Local.DownloadState)
				if p := st.Local.DownloadProgress; p != nil && p.BytesTotal > 0 {
					fmt.Printf(" — %s %.1f%% (%d / %d MB)", p.CurrentFile,
						float64(p.BytesDone)/float64(p.BytesTotal)*100,
						p.BytesDone>>20, p.BytesTotal>>20)
				}
				fmt.Println()
			}
			if st.Local.DownloadError != "" {
				fmt.Printf("download error: %s\n", st.Local.DownloadError)
			}
			return nil
		})
	},
}

var aiChatSystem string
var aiChatMaxTokens int

var aiChatCmd = &cobra.Command{
	Use:   "chat <prompt>",
	Short: "Run one chat completion and print the reply",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			resp, err := app.AIChat(ctx, service.AIChatRequest{
				Prompt:    strings.Join(args, " "),
				System:    aiChatSystem,
				MaxTokens: aiChatMaxTokens,
			})
			if err != nil {
				return err
			}
			fmt.Println(resp.Content)
			fmt.Printf("\n[%s · %s · %d+%d tokens · %.1fs]\n",
				resp.Mode, resp.Model, resp.PromptTokens, resp.CompletionTokens,
				float64(resp.DurationMs)/1000)
			return nil
		})
	},
}

var aiTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Round-trip test: hello world + a context-grounding check",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			fmt.Println("→ hello-world round trip…")
			resp, err := app.AIChat(ctx, service.AIChatRequest{
				Prompt: "Reply with exactly: Hello world, from <model family you are>.",
			})
			if err != nil {
				return err
			}
			fmt.Printf("  %s\n", strings.TrimSpace(resp.Content))

			fmt.Println("→ context-grounding check…")
			resp2, err := app.AIChat(ctx, service.AIChatRequest{
				System: "You are Heya's media assistant. The user's favorite film is Blade Runner (1982). Answer in one short sentence.",
				Prompt: "What is my favorite film?",
			})
			if err != nil {
				return err
			}
			fmt.Printf("  %s\n", strings.TrimSpace(resp2.Content))
			ok := strings.Contains(strings.ToLower(resp2.Content), "blade runner")
			fmt.Printf("\ncontext honored: %v · mode=%s · model=%s\n", ok, resp2.Mode, resp2.Model)
			if !ok {
				return fmt.Errorf("model ignored the system context")
			}
			return nil
		})
	},
}

var aiModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List selectable models for the active provider",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			models, err := app.AIModels(ctx)
			if err != nil {
				return err
			}
			for _, m := range models {
				fmt.Println(m)
			}
			return nil
		})
	},
}

var aiDownloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download the local llama-server build + selected model (blocking)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			done := make(chan error, 1)
			go func() { done <- app.AIDownloadLocalWait(ctx) }()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case err := <-done:
					if err != nil {
						return err
					}
					fmt.Println("\nlocal runtime ready")
					return nil
				case <-ticker.C:
					st := app.AIStatus(ctx)
					if p := st.Local.DownloadProgress; p != nil && p.BytesTotal > 0 {
						fmt.Printf("\r%-52s %6.1f%% (%d / %d MB)   ", p.CurrentFile,
							float64(p.BytesDone)/float64(p.BytesTotal)*100,
							p.BytesDone>>20, p.BytesTotal>>20)
					}
				}
			}
		})
	},
}

var aiStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local llama-server (reclaims RAM)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return withApp(func(ctx context.Context, app *service.App) error {
			app.AIStopLocal()
			fmt.Println("stopped")
			return nil
		})
	},
}

var aiProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "List provider presets",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, p := range llm.Providers {
			key := ""
			if p.NeedsKey {
				key = " (needs key)"
			}
			fmt.Printf("%-12s %-32s %s%s\n", p.ID, p.Label, p.BaseURL, key)
		}
		return nil
	},
}

func init() {
	aiChatCmd.Flags().StringVar(&aiChatSystem, "system", "", "optional system prompt / context")
	aiChatCmd.Flags().IntVar(&aiChatMaxTokens, "max-tokens", 0, "cap completion length")
	aiCmd.AddCommand(aiStatusCmd, aiChatCmd, aiTestCmd, aiModelsCmd, aiDownloadCmd, aiStopCmd, aiProvidersCmd)
	rootCmd.AddCommand(aiCmd)
}
