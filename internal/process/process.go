package process

import (
	"regexp"
	"strings"
)

type Result struct {
	ExitCode   *int
	Signal     *string
	Stdout     string
	Stderr     string
	TimedOut   bool
	DurationMS int64
	SpawnError string
}

var cmdMetacharPattern = regexp.MustCompile(`[(){}\[\]%!^"<>&|;,]`)

// cmdBatchArguments constructs the arguments for cmd.exe when executing a .cmd
// or .bat script. The result follows the form /d /v:off /s /c "inner", where
// inner contains the script path and arguments escaped for both cmd.exe and
// the Windows CommandLineToArgvW parser.
func cmdBatchArguments(file string, args []string) string {
	parts := []string{escapeForCmdArg(file)}
	for _, arg := range args {
		parts = append(parts, escapeForCmdArg(arg))
	}
	return `/d /v:off /s /c "` + strings.Join(parts, " ") + `"`
}

// escapeForCmdArg applies Windows CommandLineToArgvW quoting rules and ensures
// cmd.exe metacharacters (&, |, <, >, etc.) are enclosed in double quotes.
// Inside double quotes cmd.exe treats these characters as literal text rather
// than operators, preventing command injection while preserving the exact
// argument value that the target CLI receives.
func escapeForCmdArg(value string) string {
	needsQuote := strings.ContainsAny(value, " \t\"") || cmdMetacharPattern.MatchString(value)
	if !needsQuote {
		return value
	}

	// Count trailing backslashes so they can be doubled before the closing
	// quote — CommandLineToArgvW treats 2n backslashes + " as n literal
	// backslashes + end-of-string.
	trailingSlashes := 0
	for i := len(value) - 1; i >= 0 && value[i] == '\\'; i-- {
		trailingSlashes++
	}

	var b strings.Builder
	b.WriteByte('"')

	i := 0
	for i < len(value) {
		if value[i] == '\\' {
			// Count the run of consecutive backslashes.
			start := i
			for i < len(value) && value[i] == '\\' {
				i++
			}
			slashCount := i - start

			if i < len(value) && value[i] == '"' {
				// n backslashes before a literal quote become 2n backslashes
				// plus an escaped quote (\"), so CommandLineToArgvW yields
				// n backslashes followed by a literal ".
				for j := 0; j < slashCount; j++ {
					b.WriteString(`\\`)
				}
				b.WriteString(`\"`)
				i++
			} else {
				// Backslashes not followed by a quote are emitted verbatim;
				// only trailing backslashes (before the closing ") need doubling.
				for j := 0; j < slashCount; j++ {
					b.WriteByte('\\')
				}
			}
		} else if value[i] == '"' {
			b.WriteString(`\"`)
			i++
		} else {
			b.WriteByte(value[i])
			i++
		}
	}

	// Double trailing backslashes so the closing quote is not misinterpreted.
	for j := 0; j < trailingSlashes; j++ {
		b.WriteByte('\\')
	}

	b.WriteByte('"')
	return b.String()
}
