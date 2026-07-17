package process

import "testing"

func TestCmdBatchArgumentsDoubleEscapesMetacharacters(t *testing.T) {
	got := cmdBatchArguments("x.cmd", []string{"a&b"})
	want := `/d /v:off /s /c "x.cmd ^^^"a^^^&b^^^""`
	if got != want {
		t.Fatalf("cmdBatchArguments=%q; want %q", got, want)
	}
}

func TestCmdBatchArgumentsEscapesPathSpaces(t *testing.T) {
	got := cmdBatchArguments(`C:\directory with spaces\fixture.cmd`, nil)
	want := `/d /v:off /s /c "C:\directory^ with^ spaces\fixture.cmd"`
	if got != want {
		t.Fatalf("cmdBatchArguments=%q; want %q", got, want)
	}
}
