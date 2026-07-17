package dashboard

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	tabStyle       = lipgloss.NewStyle().Padding(0, 2)
	activeTabStyle = lipgloss.NewStyle().Padding(0, 2).Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#7C6DD8"))
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7C6DD8"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	boxStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#444444")).Padding(0, 1)
	labelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Width(14)
	countStyle   = lipgloss.NewStyle().Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E0636F"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#73D08A"))
)

type tab int

const (
	tabOverview tab = iota
	tabLibraries
	tabQueue
	tabWatchers
)

var tabNames = []string{"Overview", "Libraries", "Queue", "Watchers"}

type Model struct {
	active    tab
	client    *Client
	width     int
	height    int
	startTime time.Time
	err       error

	libraries []LibraryData
	watchers  []WatcherEntry
	fileStats map[int64]map[string]int64
}

type tickMsg time.Time
type dataMsg struct {
	libraries []LibraryData
	watchers  []WatcherEntry
	fileStats map[int64]map[string]int64
	err       error
}

func New(serverURL, token string) Model {
	return NewWithClient(NewClient(serverURL, token))
}

func NewWithClient(client *Client) Model {
	return Model{
		client:    client,
		startTime: time.Now(),
		fileStats: make(map[int64]map[string]int64),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchData(m.client), tick())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "right":
			m.active = (m.active + 1) % tab(len(tabNames))
		case "shift+tab", "left":
			m.active = (m.active - 1 + tab(len(tabNames))) % tab(len(tabNames))
		case "1":
			m.active = tabOverview
		case "2":
			m.active = tabLibraries
		case "3":
			m.active = tabQueue
		case "4":
			m.active = tabWatchers
		case "r":
			return m, fetchData(m.client)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(fetchData(m.client), tick())

	case dataMsg:
		m.err = msg.err
		if msg.err == nil {
			m.libraries = msg.libraries
			m.watchers = msg.watchers
			m.fileStats = msg.fileStats
		}
	}

	return m, nil
}

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true

	if m.width == 0 {
		v.Content = "Loading..."
		return v
	}

	var sb strings.Builder

	sb.WriteString(titleStyle.Render(" Heya Dashboard"))
	sb.WriteString(dimStyle.Render("  q quit  r refresh  1-4 tabs"))
	sb.WriteString("\n")

	sb.WriteString(m.renderTabs())
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", min(m.width, 80)))
	sb.WriteString("\n\n")

	if m.err != nil {
		sb.WriteString(errorStyle.Render("  Connection error: " + m.err.Error()))
		sb.WriteString("\n")
		v.Content = sb.String()
		return v
	}

	switch m.active {
	case tabOverview:
		sb.WriteString(m.renderOverview())
	case tabLibraries:
		sb.WriteString(m.renderLibraries())
	case tabQueue:
		sb.WriteString(m.renderQueue())
	case tabWatchers:
		sb.WriteString(m.renderWatchers())
	}

	v.Content = sb.String()
	return v
}

func (m Model) renderTabs() string {
	var tabs []string
	for i, name := range tabNames {
		if tab(i) == m.active {
			tabs = append(tabs, activeTabStyle.Render(name))
		} else {
			tabs = append(tabs, tabStyle.Render(name))
		}
	}
	return " " + strings.Join(tabs, " ")
}

func (m Model) renderOverview() string {
	var sb strings.Builder
	uptime := time.Since(m.startTime).Round(time.Second)

	sb.WriteString(fmt.Sprintf("  %s %s  %s %s\n\n",
		labelStyle.Render("Server:"), successStyle.Render("Running"),
		labelStyle.Render("Uptime:"), dimStyle.Render(uptime.String())))

	var totalFiles int64
	var totalMatched int64
	for _, stats := range m.fileStats {
		for status, count := range stats {
			totalFiles += count
			if status == "matched" {
				totalMatched += count
			}
		}
	}

	mediaBox := boxStyle.Render(
		titleStyle.Render("Media") + "\n" +
			fmt.Sprintf("  %s %s\n", labelStyle.Render("Libraries:"), countStyle.Render(fmt.Sprintf("%d", len(m.libraries)))) +
			fmt.Sprintf("  %s %s\n", labelStyle.Render("Total Files:"), countStyle.Render(fmt.Sprintf("%d", totalFiles))) +
			fmt.Sprintf("  %s %s", labelStyle.Render("Matched:"), countStyle.Render(fmt.Sprintf("%d", totalMatched))),
	)

	watcherBox := boxStyle.Render(
		titleStyle.Render("System") + "\n" +
			fmt.Sprintf("  %s %s\n", labelStyle.Render("Watchers:"), countStyle.Render(fmt.Sprintf("%d active", len(m.watchers)))) +
			fmt.Sprintf("  %s %s", labelStyle.Render("Libraries:"), countStyle.Render(fmt.Sprintf("%d", len(m.libraries)))),
	)

	sb.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, "  "+mediaBox, "  "+watcherBox))
	sb.WriteString("\n")

	return sb.String()
}

func (m Model) renderLibraries() string {
	if len(m.libraries) == 0 {
		return "  No libraries configured."
	}

	var sb strings.Builder
	for _, lib := range m.libraries {
		badge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).
			Background(mediaColor(lib.MediaType)).Padding(0, 1).
			Render(strings.ToUpper(lib.MediaType))

		sb.WriteString(fmt.Sprintf("  %s %s (id=%d)\n", badge, countStyle.Render(lib.Name), lib.ID))

		stats := m.fileStats[lib.ID]
		if len(stats) > 0 {
			for status, count := range stats {
				sb.WriteString(fmt.Sprintf("    %s %d\n", labelStyle.Render(status+":"), count))
			}
		}
		if len(lib.Paths) > 0 {
			sb.WriteString(fmt.Sprintf("    %s %s\n", labelStyle.Render("Paths:"), dimStyle.Render(strings.Join(lib.Paths, ", "))))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m Model) renderQueue() string {
	return "  " + dimStyle.Render("Queue monitoring via River job table — coming in next iteration.") + "\n"
}

func (m Model) renderWatchers() string {
	if len(m.watchers) == 0 {
		return "  No active watchers."
	}

	var sb strings.Builder
	for _, w := range m.watchers {
		sb.WriteString(fmt.Sprintf("  Library %-4d %s\n", w.LibraryID, w.Path))
	}
	return sb.String()
}

func mediaColor(mt string) color.Color {
	switch mt {
	case "movie":
		return lipgloss.Color("#5B9FE4")
	case "tv", "anime":
		return lipgloss.Color("#9B7DD4")
	case "music":
		return lipgloss.Color("#5BC48C")
	case "book":
		return lipgloss.Color("#D4A95B")
	default:
		return lipgloss.Color("#666666")
	}
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchData(client *Client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		libs, err := client.FetchLibraries(ctx)
		if err != nil {
			return dataMsg{err: err}
		}

		watchers, _ := client.FetchWatchers(ctx)

		fileStats := make(map[int64]map[string]int64)
		for _, lib := range libs {
			stats, err := client.FetchFileStats(ctx, lib.ID)
			if err == nil {
				fileStats[lib.ID] = stats
			}
		}

		return dataMsg{
			libraries: libs,
			watchers:  watchers,
			fileStats: fileStats,
		}
	}
}
