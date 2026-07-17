package process

import (
	"strings"
	"testing"
)

func TestEscapeForCmdArgPlainValue(t *testing.T) {
	if got := escapeForCmdArg("hello"); got != "hello" {
		t.Fatalf("plain value should pass through: got %q", got)
	}
}

func TestEscapeForCmdArgSpacesQuoted(t *testing.T) {
	got := escapeForCmdArg("hello world")
	if !strings.HasPrefix(got, "\"") || !strings.HasSuffix(got, "\"") {
		t.Fatalf("spaced value must be quoted: got %q", got)
	}
	if inner := got[1 : len(got)-1]; inner != "hello world" {
		t.Fatalf("inner content must be preserved: got %q", inner)
	}
}

func TestEscapeForCmdArgMetacharactersQuotedNotEscaped(t *testing.T) {
	for _, value := range []string{"hello & world", "a | b", "x > y", "x < y"} {
		got := escapeForCmdArg(value)
		if !strings.HasPrefix(got, "\"") || !strings.HasSuffix(got, "\"") {
			t.Fatalf("metachar value %q must be quoted: got %q", value, got)
		}
		// Inside quotes the original value must appear verbatim — no literal
		// quotes or caret escapes injected into the argument.
		inner := got[1 : len(got)-1]
		if inner != value {
			t.Fatalf("metachar value %q must be preserved inside quotes: inner=%q", value, inner)
		}
	}
}

func TestEscapeForCmdArgEmbeddedQuote(t *testing.T) {
	got := escapeForCmdArg(`say "hi"`)
	// The embedded quotes should be backslash-escaped, outer quotes added.
	if got != `"say \"hi\""` {
		t.Fatalf("embedded quote escaping: got %q", got)
	}
}

func TestEscapeForCmdArgTrailingBackslashWithSpace(t *testing.T) {
	// Trailing backslash doubling only matters when quoting is triggered for
	// another reason (here: the space). CommandLineToArgvW treats 2n
	// backslashes before a closing quote as n literal backslashes.
	got := escapeForCmdArg(`C:\my path\`)
	if got != `"C:\my path\\"` {
		t.Fatalf("trailing backslash with space: got %q", got)
	}
}

func TestCmdBatchArgumentsStructure(t *testing.T) {
	got := cmdBatchArguments(`C:\test.cmd`, []string{"hello & world"})
	if !strings.HasPrefix(got, `/d /v:off /s /c "`) {
		t.Fatalf("missing cmd.exe prefix: got %q", got)
	}
	if !strings.HasSuffix(got, `"`) {
		t.Fatalf("missing closing quote: got %q", got)
	}
	// The argument value must appear inside the command string without literal
	// double quotes wrapping it (the outer /c quotes are consumed by cmd.exe
	// with /s).
	inner := got[len(`/d /v:off /s /c "`):]
	inner = inner[:len(inner)-1] // strip closing quote
	if !strings.Contains(inner, `"hello & world"`) {
		t.Fatalf("argument must be quoted inside cmd string: inner=%q", inner)
	}
}
