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

var (
	cmdMetaPattern          = regexp.MustCompile("[()\\[\\]%!^\"`<>&|;, *?]")
	cmdQuotePattern         = regexp.MustCompile(`(\\*)"`)
	cmdTrailingSlashPattern = regexp.MustCompile(`(\\*)$`)
)

func cmdBatchArguments(file string, args []string) string {
	escapeMeta := func(value string) string {
		return cmdMetaPattern.ReplaceAllString(value, "^${0}")
	}
	escapeArg := func(value string) string {
		value = cmdQuotePattern.ReplaceAllString(value, `$1$1\"`)
		value = cmdTrailingSlashPattern.ReplaceAllString(value, "$1$1")
		return escapeMeta(escapeMeta(`"` + value + `"`))
	}
	parts := []string{escapeMeta(file)}
	for _, arg := range args {
		parts = append(parts, escapeArg(arg))
	}
	return `/d /v:off /s /c "` + strings.Join(parts, " ") + `"`
}
