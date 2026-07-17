package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JeremyDev87/theseus/internal/contract"
	"github.com/JeremyDev87/theseus/internal/report"
	"github.com/JeremyDev87/theseus/internal/runner"
	"github.com/JeremyDev87/theseus/internal/version"
)

var help = fmt.Sprintf(`Theseus %s

Distribution parity tests for packaged CLIs.

Usage:
  theseus verify <package|directory|tgz> --contract <file> [--format text|json]
  theseus --help
  theseus --version

Exit codes:
  0  all declared contracts hold
  1  behavior drift
  2  contract, pack, install, or probe incomplete
`, version.Version)

type verifyArgs struct {
	Target       string
	ContractPath string
	Format       string
}

type errorReport struct {
	SchemaVersion int       `json:"schemaVersion"`
	Tool          tool      `json:"tool"`
	Target        string    `json:"target"`
	Status        string    `json:"status"`
	ExitCode      int       `json:"exitCode"`
	Error         errorBody `json:"error"`
}
type tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func Main(argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 || contains(argv, "--help") || argv[0] == "help" {
		fmt.Fprint(stdout, help)
		return 0
	}
	if len(argv) == 1 && argv[0] == "--version" {
		fmt.Fprintln(stdout, version.Version)
		return 0
	}
	if argv[0] != "verify" {
		fmt.Fprintf(stderr, "theseus: unknown command %s\n", argv[0])
		return 2
	}
	parsed, err := parseVerify(argv[1:])
	if err != nil {
		fmt.Fprintf(stderr, "theseus: %s\n", err)
		return 2
	}
	content, err := os.ReadFile(filepath.Clean(parsed.ContractPath))
	if err != nil {
		return writeFailure(parsed, err.Error(), false, stdout, stderr)
	}
	if !json.Valid(content) {
		var value any
		parseErr := json.Unmarshal(content, &value)
		return writeFailure(parsed, "contract JSON is invalid: "+parseErr.Error(), true, stdout, stderr)
	}
	config, err := contract.Parse(content)
	if err != nil {
		return writeFailure(parsed, err.Error(), true, stdout, stderr)
	}
	result, err := runner.Verify(parsed.Target, config)
	if err != nil {
		return writeFailure(parsed, err.Error(), false, stdout, stderr)
	}
	finalReport := report.Create(parsed.Target, result)
	if parsed.Format == "json" {
		data, err := json.MarshalIndent(finalReport, "", "  ")
		if err != nil {
			return writeFailure(parsed, err.Error(), false, stdout, stderr)
		}
		fmt.Fprintf(stdout, "%s\n", data)
	} else {
		fmt.Fprint(stdout, report.Text(finalReport))
	}
	return finalReport.ExitCode
}

func parseVerify(argv []string) (verifyArgs, error) {
	if len(argv) == 0 || strings.HasPrefix(argv[0], "-") {
		return verifyArgs{}, errors.New("verify requires a package, directory, or .tgz target")
	}
	result := verifyArgs{Target: argv[0], ContractPath: "theseus.config.json", Format: "text"}
	for index := 1; index < len(argv); index++ {
		switch argv[index] {
		case "--contract":
			if index+1 >= len(argv) {
				return verifyArgs{}, errors.New("--contract requires a file path")
			}
			index++
			result.ContractPath = argv[index]
		case "--format":
			if index+1 >= len(argv) {
				return verifyArgs{}, errors.New("--format must be text or json")
			}
			index++
			if argv[index] != "text" && argv[index] != "json" {
				return verifyArgs{}, errors.New("--format must be text or json")
			}
			result.Format = argv[index]
		default:
			return verifyArgs{}, fmt.Errorf("unknown option %s", argv[index])
		}
	}
	return result, nil
}

func writeFailure(parsed verifyArgs, detail string, contractFailure bool, stdout, stderr io.Writer) int {
	if parsed.Format == "json" {
		payload := errorReport{SchemaVersion: 1, Tool: tool{Name: "theseus", Version: version.Version}, Target: parsed.Target, Status: "incomplete", ExitCode: 2, Error: errorBody{Code: "THS-INCOMPLETE-001", Message: detail}}
		data, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Fprintf(stdout, "%s\n", data)
	} else {
		prefix := "incomplete"
		if contractFailure {
			prefix = "contract"
		}
		fmt.Fprintf(stderr, "theseus %s: %s\n", prefix, detail)
	}
	return 2
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
