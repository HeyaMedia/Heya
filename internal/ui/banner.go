package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

var Version = "dev"

const logo = `
 ╔═╗╔═╗╔═╗╔═╗
 ║ ╠╝ ╠╝ ║║ ╠╗
 ║ ║  ║  ║║ ╠╝╔═╗
 ╚═╝  ╚══╝╚═╝ ╚═╝`

func Banner() string {
	if !ColorEnabled {
		return fmt.Sprintf("Heya v%s — self-hosted media server\n", Version)
	}

	logoStyled := lipgloss.NewStyle().
		Foreground(ColorPrimary).
		Bold(true).
		Render(logo)

	tagline := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true).
		Render(fmt.Sprintf("  v%s — self-hosted media server", Version))

	return logoStyled + "\n" + tagline + "\n"
}

func HelpBanner() string {
	return Banner() + "\n" +
		Bold("Commands:") + "\n" +
		fmt.Sprintf("  %s  %s\n", Primary("serve"), "Start the HTTP server and background workers") +
		fmt.Sprintf("  %s  %s\n", Primary("setup"), "Guided first-time configuration") +
		fmt.Sprintf("  %s  %s\n", Primary("dashboard"), "Live server dashboard (TUI)") +
		"\n" +
		Bold("Management:") + "\n" +
		fmt.Sprintf("  %s  %s\n", Primary("library"), "Manage media libraries (add, scan, list, info)") +
		fmt.Sprintf("  %s  %s\n", Primary("media"), "Browse and search media items") +
		fmt.Sprintf("  %s  %s\n", Primary("user"), "Manage users") +
		fmt.Sprintf("  %s  %s\n", Primary("config"), "View and edit configuration") +
		"\n" +
		Bold("Tools:") + "\n" +
		fmt.Sprintf("  %s  %s\n", Primary("parse"), "Parse a media filename or directory") +
		fmt.Sprintf("  %s  %s\n", Primary("migrate"), "Run database migrations") +
		fmt.Sprintf("  %s  %s\n", Primary("job"), "View background job status") +
		"\n" +
		Dim("Run 'heya <command> --help' for details on any command.") +
		"\n"
}
