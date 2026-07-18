package ui

import (
	"fmt"
	"os"
)

func Success(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if ColorEnabled {
		_, _ = fmt.Fprintf(os.Stdout, "%s %s\n", StyleSuccess.Render("✓"), text)
	} else {
		_, _ = fmt.Fprintf(os.Stdout, "[OK] %s\n", text)
	}
}

func Error(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if ColorEnabled {
		fmt.Fprintf(os.Stderr, "%s %s\n", StyleError.Render("✗"), text)
	} else {
		fmt.Fprintf(os.Stderr, "[ERROR] %s\n", text)
	}
}

func Warn(msg string, args ...any) {
	text := fmt.Sprintf(msg, args...)
	if ColorEnabled {
		fmt.Fprintf(os.Stderr, "%s %s\n", StyleWarn.Render("!"), text)
	} else {
		fmt.Fprintf(os.Stderr, "[WARN] %s\n", text)
	}
}

func Header(msg string) {
	if ColorEnabled {
		fmt.Println(StyleHeader.Render(msg))
	} else {
		fmt.Printf("=== %s ===\n", msg)
	}
}

func Info(label, value string) {
	if ColorEnabled {
		fmt.Printf("%s %s\n", StyleLabel.Render(label+":"), value)
	} else {
		fmt.Printf("%-16s %s\n", label+":", value)
	}
}

func Dim(msg string) string {
	if ColorEnabled {
		return StyleDim.Render(msg)
	}
	return msg
}

func Bold(msg string) string {
	if ColorEnabled {
		return StyleBold.Render(msg)
	}
	return msg
}

func Primary(msg string) string {
	if ColorEnabled {
		return StylePrimary.Render(msg)
	}
	return msg
}

func Println(msg string) {
	fmt.Println(msg)
}

func Printf(format string, args ...any) {
	fmt.Printf(format, args...)
}
