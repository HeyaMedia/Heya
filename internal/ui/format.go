package ui

import (
	"encoding/json"
	"os"
)

var (
	JSONMode     bool
	ColorEnabled bool
)

func Init(jsonFlag, noColorFlag bool) {
	JSONMode = jsonFlag

	if noColorFlag || JSONMode {
		ColorEnabled = false
		return
	}

	ColorEnabled = isTerminal()
}

func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func IsInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func OutputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
