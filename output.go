package log

import (
	"fmt"
	"strings"
)

type Output int

const (
	OutputConsole Output = iota
	OutputFile
	OutputMmap
)

var outputMap = map[string]Output{
	"console": OutputConsole,
	"file":    OutputFile,
}

// UnmarshalText Unmarshal the text.
func (o *Output) UnmarshalText(text []byte) error {
	output, ok := outputMap[strings.ToLower(string(text))]
	if !ok {
		return fmt.Errorf("not support output: %v", string(text))
	}
	*o = output
	return nil
}
