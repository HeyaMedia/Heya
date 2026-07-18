package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

type Table struct {
	headers []string
	rows    [][]string
}

func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

func (t *Table) AddRow(cells ...string) *Table {
	t.rows = append(t.rows, cells)
	return t
}

func (t *Table) Render() string {
	if len(t.rows) == 0 {
		return Dim("No results.")
	}

	if !ColorEnabled {
		return t.renderPlain()
	}

	tbl := table.New().
		Headers(t.headers...).
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ColorDim)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Bold(true).Foreground(ColorPrimary).Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})

	for _, row := range t.rows {
		tbl.Row(row...)
	}

	return tbl.Render()
}

func (t *Table) renderPlain() string {
	if len(t.rows) == 0 {
		return "No results."
	}

	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var sb strings.Builder
	for i, h := range t.headers {
		if i > 0 {
			sb.WriteString("  ")
		}
		fmt.Fprintf(&sb, "%-*s", widths[i], h)
	}
	sb.WriteString("\n")

	for _, row := range t.rows {
		for i, cell := range row {
			if i > 0 {
				sb.WriteString("  ")
			}
			if i < len(widths) {
				fmt.Fprintf(&sb, "%-*s", widths[i], cell)
			} else {
				sb.WriteString(cell)
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
