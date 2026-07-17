package compare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/contract"
	"github.com/JeremyDev87/theseus/internal/model"
)

func TestLanguageNeutralGolden(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "conformance")
	for _, name := range []string{"basic", "allowed"} {
		t.Run(name, func(t *testing.T) {
			contractBytes, err := os.ReadFile(filepath.Join(root, "contracts", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			config, err := contract.Parse(contractBytes)
			if err != nil {
				t.Fatal(err)
			}
			var receipts []model.RunReceipt
			readJSON(t, filepath.Join(root, "receipts", name+".json"), &receipts)
			var expected model.ComparisonResult
			readJSON(t, filepath.Join(root, "expected", name+".json"), &expected)
			actual := Receipts(config, receipts)
			actualCanonical, actualErr := canonical.Marshal(actual)
			expectedCanonical, expectedErr := canonical.Marshal(expected)
			if actualErr != nil || expectedErr != nil {
				t.Fatalf("canonicalization failed: actual=%v expected=%v", actualErr, expectedErr)
			}
			if actualCanonical != expectedCanonical {
				actualJSON, _ := json.MarshalIndent(actual, "", "  ")
				expectedJSON, _ := json.MarshalIndent(expected, "", "  ")
				t.Fatalf("golden drift\nactual=%s\nexpected=%s", actualJSON, expectedJSON)
			}
		})
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
