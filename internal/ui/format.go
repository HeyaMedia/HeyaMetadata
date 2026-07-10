package ui

import (
	"encoding/json"
	"os"
)

var (
	JSONMode     bool
	ColorEnabled bool
)

func Init(jsonMode, noColor bool) {
	JSONMode = jsonMode
	ColorEnabled = !jsonMode && !noColor && isTerminal(os.Stdout)
}

func OutputJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
