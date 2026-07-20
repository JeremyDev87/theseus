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
	"github.com/JeremyDev87/theseus/internal/evidenceid"
	"github.com/JeremyDev87/theseus/internal/report"
	"github.com/JeremyDev87/theseus/internal/reportdiff"
	"github.com/JeremyDev87/theseus/internal/runner"
	"github.com/JeremyDev87/theseus/internal/version"
)

var help = fmt.Sprintf(`Theseus %s

Distribution parity tests for packaged CLIs.

Usage:
  theseus verify <package|directory|tgz> --contract <file> [--format text|json]
  theseus diff <before.json> <after.json> [--format text|json]
  theseus --help
  theseus --version

Exit codes:
  0  all declared contracts hold, or no evidence was introduced
  1  behavior drift, or evidence was introduced
  2  input, contract, pack, install, or probe incomplete
`, version.Version)

type verifyArgs struct {
	Target       string
	ContractPath string
	Format       string
}

type diffArgs struct {
	BeforePath string
	AfterPath  string
	Format     string
}

type errorReport struct {
	SchemaVersion   int       `json:"schemaVersion"`
	IdentityVersion int       `json:"identityVersion"`
	Tool            tool      `json:"tool"`
	Target          string    `json:"target"`
	Status          string    `json:"status"`
	ExitCode        int       `json:"exitCode"`
	Error           errorBody `json:"error"`
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
	switch argv[0] {
	case "verify":
		return runVerify(argv[1:], stdout, stderr)
	case "diff":
		return runDiff(argv[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "theseus: unknown command %s\n", argv[0])
		return 2
	}
}

func runVerify(argv []string, stdout, stderr io.Writer) int {
	parsed, err := parseVerify(argv)
	if err != nil {
		fmt.Fprintf(stderr, "theseus: %s\n", err)
		return 2
	}
	content, err := os.ReadFile(filepath.Clean(parsed.ContractPath))
	if err != nil {
		return writeFailure(parsed.Target, parsed.Format, err.Error(), false, stdout, stderr)
	}
	if !json.Valid(content) {
		var value any
		parseErr := json.Unmarshal(content, &value)
		return writeFailure(parsed.Target, parsed.Format, "contract JSON is invalid: "+parseErr.Error(), true, stdout, stderr)
	}
	config, err := contract.Parse(content)
	if err != nil {
		return writeFailure(parsed.Target, parsed.Format, err.Error(), true, stdout, stderr)
	}
	result, err := runner.Verify(parsed.Target, config)
	if err != nil {
		return writeFailure(parsed.Target, parsed.Format, err.Error(), false, stdout, stderr)
	}
	finalReport := report.Create(parsed.Target, result)
	if parsed.Format == "json" {
		data, err := json.MarshalIndent(finalReport, "", "  ")
		if err != nil {
			return writeFailure(parsed.Target, parsed.Format, err.Error(), false, stdout, stderr)
		}
		fmt.Fprintf(stdout, "%s\n", data)
	} else {
		fmt.Fprint(stdout, report.Text(finalReport))
	}
	return finalReport.ExitCode
}

func runDiff(argv []string, stdout, stderr io.Writer) int {
	parsed, err := parseDiff(argv)
	if err != nil {
		fmt.Fprintf(stderr, "theseus: %s\n", err)
		return 2
	}
	target := parsed.BeforePath + " -> " + parsed.AfterPath
	beforeBytes, err := os.ReadFile(filepath.Clean(parsed.BeforePath))
	if err != nil {
		return writeFailure(target, parsed.Format, "before report: "+err.Error(), false, stdout, stderr)
	}
	before, err := reportdiff.Parse(beforeBytes)
	if err != nil {
		return writeFailure(target, parsed.Format, "before report: "+err.Error(), false, stdout, stderr)
	}
	afterBytes, err := os.ReadFile(filepath.Clean(parsed.AfterPath))
	if err != nil {
		return writeFailure(target, parsed.Format, "after report: "+err.Error(), false, stdout, stderr)
	}
	after, err := reportdiff.Parse(afterBytes)
	if err != nil {
		return writeFailure(target, parsed.Format, "after report: "+err.Error(), false, stdout, stderr)
	}
	result, err := reportdiff.Compare(before, after)
	if err != nil {
		return writeFailure(target, parsed.Format, err.Error(), false, stdout, stderr)
	}
	if parsed.Format == "json" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return writeFailure(target, parsed.Format, err.Error(), false, stdout, stderr)
		}
		fmt.Fprintf(stdout, "%s\n", data)
	} else {
		fmt.Fprint(stdout, reportdiff.Text(result))
	}
	return result.ExitCode
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
			format, next, err := parseFormat(argv, index)
			if err != nil {
				return verifyArgs{}, err
			}
			result.Format, index = format, next
		default:
			return verifyArgs{}, fmt.Errorf("unknown option %s", argv[index])
		}
	}
	return result, nil
}

func parseDiff(argv []string) (diffArgs, error) {
	if len(argv) < 2 || strings.HasPrefix(argv[0], "-") || strings.HasPrefix(argv[1], "-") {
		return diffArgs{}, errors.New("diff requires before.json and after.json report paths")
	}
	result := diffArgs{BeforePath: argv[0], AfterPath: argv[1], Format: "text"}
	for index := 2; index < len(argv); index++ {
		switch argv[index] {
		case "--format":
			format, next, err := parseFormat(argv, index)
			if err != nil {
				return diffArgs{}, err
			}
			result.Format, index = format, next
		default:
			return diffArgs{}, fmt.Errorf("unknown option %s", argv[index])
		}
	}
	return result, nil
}

func parseFormat(argv []string, index int) (string, int, error) {
	if index+1 >= len(argv) {
		return "", index, errors.New("--format must be text or json")
	}
	index++
	if argv[index] != "text" && argv[index] != "json" {
		return "", index, errors.New("--format must be text or json")
	}
	return argv[index], index, nil
}

func writeFailure(target, format, detail string, contractFailure bool, stdout, stderr io.Writer) int {
	if format == "json" {
		payload := errorReport{
			SchemaVersion: 2, IdentityVersion: evidenceid.Version,
			Tool: tool{Name: "theseus", Version: version.Version}, Target: target,
			Status: "incomplete", ExitCode: 2,
			Error: errorBody{Code: "THS-INCOMPLETE-001", Message: detail},
		}
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
