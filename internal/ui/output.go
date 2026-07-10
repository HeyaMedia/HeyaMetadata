package ui

import (
	"fmt"
	"io"
	"os"
)

func Success(message string, args ...any) {
	prefixed(os.Stdout, StyleSuccess, "✓", "OK", message, args...)
}

func Error(message string, args ...any) {
	prefixed(os.Stderr, StyleError, "✗", "ERROR", message, args...)
}

func Warn(message string, args ...any) {
	prefixed(os.Stderr, StyleWarn, "!", "WARN", message, args...)
}

func Info(label, value string) {
	if ColorEnabled {
		fmt.Fprintf(os.Stdout, "%s %s\n", StyleLabel.Render(label+":"), value)
		return
	}
	fmt.Fprintf(os.Stdout, "%-14s %s\n", label+":", value)
}

func Primary(value string) string {
	if ColorEnabled {
		return StylePrimary.Render(value)
	}
	return value
}

func Bold(value string) string {
	if ColorEnabled {
		return StyleBold.Render(value)
	}
	return value
}

func Dim(value string) string {
	if ColorEnabled {
		return StyleDim.Render(value)
	}
	return value
}

func prefixed(writer io.Writer, style lipglossStyle, symbol, fallback, message string, args ...any) {
	text := fmt.Sprintf(message, args...)
	if ColorEnabled {
		fmt.Fprintf(writer, "%s %s\n", style.Render(symbol), text)
		return
	}
	fmt.Fprintf(writer, "[%s] %s\n", fallback, text)
}

type lipglossStyle interface {
	Render(...string) string
}
