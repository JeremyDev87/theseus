package runtimeidentity

import (
	"testing"

	"github.com/JeremyDev87/theseus/internal/model"
)

func TestCreateCanonicalRuntimeIdentity(t *testing.T) {
	tests := []struct {
		name          string
		input         model.RuntimeIdentity
		wantCanonical string
		wantSHA256    string
	}{
		{
			name: "source",
			input: model.RuntimeIdentity{
				Kind: "source", BinName: "fixture", ExecutablePath: "./bin.js", ExecutableRealPath: "./bin.js",
				OptionalDependencies: []model.OptionalDependencyReceipt{},
			},
			wantCanonical: `{"binName":"fixture","executablePath":"./bin.js","executableRealPath":"./bin.js","kind":"source","optionalDependencies":[]}`,
			wantSHA256:    "c0737f0d3a6f4e153481aadb41b40435838dba590db35d12ca2f634384139138",
		},
		{
			name: "installed",
			input: model.RuntimeIdentity{
				Kind: "installed", PackageName: "fixture", PackageVersion: "1.0.0", BinName: "fixture",
				ExecutablePath: "node_modules/.bin/fixture", ExecutableRealPath: "node_modules/fixture/bin.js",
				OptionalDependencies: []model.OptionalDependencyReceipt{{Name: "@scope/native", Version: "1.2.3"}},
			},
			wantCanonical: `{"binName":"fixture","executablePath":"node_modules/.bin/fixture","executableRealPath":"node_modules/fixture/bin.js","kind":"installed","optionalDependencies":[{"name":"@scope/native","version":"1.2.3"}],"packageName":"fixture","packageVersion":"1.0.0"}`,
			wantSHA256:    "9e3d632676c83bb1eca65328b3c48c5f7d5af47a62c892c3f06903c8c32f50ef",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Create(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Canonical != test.wantCanonical || got.SHA256 != test.wantSHA256 {
				t.Fatalf("canonical=%q sha256=%q", got.Canonical, got.SHA256)
			}
		})
	}
}

func TestCreateRejectsUnknownKind(t *testing.T) {
	if _, err := Create(model.RuntimeIdentity{Kind: "other", OptionalDependencies: []model.OptionalDependencyReceipt{}}); err == nil {
		t.Fatal("unknown runtime kind accepted")
	}
}
