package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/karbowiak/heya/internal/parser"
	"github.com/spf13/cobra"
)

var parseCmd = &cobra.Command{
	Use:   "parse [name]",
	Short: "Parse a media filename or path",
	Long:  "Parse a release name or filesystem path and display the extracted metadata.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dirPath, _ := cmd.Flags().GetString("path")
		asJSON, _ := cmd.Flags().GetBool("json")

		if len(args) == 0 && dirPath == "" {
			return fmt.Errorf("provide a release name as an argument or use --path")
		}

		if dirPath != "" {
			return parseDirectory(dirPath, asJSON)
		}

		return parseReleaseName(args[0], asJSON)
	},
}

func init() {
	parseCmd.Flags().String("path", "", "Parse all files in a directory")
	parseCmd.Flags().Bool("json", false, "Output as JSON")
}

func parseReleaseName(name string, asJSON bool) error {
	result := parser.ParseSceneReleaseName(name, parser.MediaUnknown)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if result == nil {
			return enc.Encode(map[string]interface{}{"input": name, "release": nil})
		}
		return enc.Encode(result)
	}

	if result == nil {
		fmt.Printf("No release parsed from: %s\n", name)
		return nil
	}

	printRelease(result)
	return nil
}

func parseDirectory(dirPath string, asJSON bool) error {
	var paths []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking %s: %w", dirPath, err)
	}

	results := parser.ParseStoragePaths(paths)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprint(w, "TYPE\tMEDIA\tSTATUS\tTITLE\tYEAR\tPATH\n"); err != nil {
		return fmt.Errorf("write parse table header: %w", err)
	}

	for _, r := range results {
		title := ""
		year := ""
		if r.Release != nil {
			title = r.Release.Title
			year = r.Release.Year
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.EntryType, r.Media, r.Status, title, year, r.Basename); err != nil {
			return fmt.Errorf("write parse table row: %w", err)
		}
	}

	return w.Flush()
}

func printRelease(r *parser.SceneReleaseParse) {
	fmt.Printf("Strategy:   %s\n", r.Strategy)
	fmt.Printf("Media:      %s\n", r.Media)
	fmt.Printf("Title:      %s\n", r.Title)
	if r.Year != "" {
		fmt.Printf("Year:       %s\n", r.Year)
	}
	if r.Group != "" {
		fmt.Printf("Group:      %s\n", r.Group)
	}
	if r.Resolution != "" {
		fmt.Printf("Resolution: %s\n", r.Resolution)
	}
	if r.Source != "" {
		fmt.Printf("Source:     %s\n", r.Source)
	}
	if len(r.Sources) > 0 {
		fmt.Printf("Sources:    %s\n", strings.Join(r.Sources, ", "))
	}
	if r.Codec != "" {
		fmt.Printf("Codec:      %s\n", r.Codec)
	}
	if r.Catalog != "" {
		fmt.Printf("Catalog:    %s\n", r.Catalog)
	}
	if r.ReleaseHash != "" {
		fmt.Printf("Hash:       %s\n", r.ReleaseHash)
	}
	if len(r.Seasons) > 0 {
		fmt.Printf("Seasons:    %v\n", r.Seasons)
	}
	if len(r.Episodes) > 0 {
		fmt.Printf("Episodes:   %v\n", r.Episodes)
	}
	if r.IsTv {
		fmt.Printf("TV:         true\n")
	}
	if len(r.Flags) > 0 {
		fmt.Printf("Flags:      %s\n", strings.Join(r.Flags, ", "))
	}
	fmt.Printf("Score:      %d\n", r.Score)
}
