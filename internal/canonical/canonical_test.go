package canonical

import "testing"

func TestMarshalSortsKeysWithoutLocale(t *testing.T) {
	got, err := Marshal(map[string]any{"ä": 3, "a": 2, "Z": 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"Z":1,"a":2,"ä":3}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestMarshalKeepsJavaScriptLineSeparatorsUnescaped(t *testing.T) {
	got, err := Marshal(map[string]any{"x": "\u2028", "y": "\u2029"})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"x\":\"\u2028\",\"y\":\"\u2029\"}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestMarshalUsesJavaScriptUTF16CodeUnitOrder(t *testing.T) {
	got, err := Marshal(map[string]any{"\ue000": 1, "😀": 2})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"😀":2,"":1}`
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
