package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/JeremyDev87/theseus/internal/canonical"
	"github.com/JeremyDev87/theseus/internal/compare"
	"github.com/JeremyDev87/theseus/internal/contract"
	"github.com/JeremyDev87/theseus/internal/model"
	processrun "github.com/JeremyDev87/theseus/internal/process"
	"github.com/JeremyDev87/theseus/internal/runtimeidentity"
)

type preparedArtifact struct {
	model.ArtifactReceipt
	TarballPath string
}

type packageManifest struct {
	Name                 string                     `json:"name"`
	Version              string                     `json:"version"`
	Bin                  json.RawMessage            `json:"bin"`
	OptionalDependencies map[string]json.RawMessage `json:"optionalDependencies"`
}

var (
	currentWorkingDirectory = os.Getwd
	removeTemporaryRoot     = os.RemoveAll
)

func Verify(target string, config model.Contract) (result model.VerificationResult, err error) {
	tempRoot, err := os.MkdirTemp("", "theseus-")
	if err != nil {
		return model.VerificationResult{}, err
	}
	defer func() {
		if cleanupErr := removeTemporaryRoot(tempRoot); cleanupErr != nil {
			result = model.VerificationResult{}
			err = errors.Join(err, fmt.Errorf("unable to remove temporary root: %w", cleanupErr))
		}
	}()
	cwd, err := currentWorkingDirectory()
	if err != nil {
		return model.VerificationResult{}, fmt.Errorf("unable to resolve working directory: %w", err)
	}
	nodeResult := processrun.Run("node", []string{"--version"}, cwd, 10*time.Second)
	if nodeResult.ExitCode == nil || *nodeResult.ExitCode != 0 || nodeResult.SpawnError != "" {
		return model.VerificationResult{}, fmt.Errorf("unable to resolve node version: %s", commandDetail(nodeResult))
	}
	npmResult := processrun.Run("npm", []string{"--version"}, cwd, 10*time.Second)
	if npmResult.ExitCode == nil || *npmResult.ExitCode != 0 || npmResult.SpawnError != "" {
		return model.VerificationResult{}, fmt.Errorf("unable to resolve npm version: %s", commandDetail(npmResult))
	}
	environment := model.EnvironmentReceipt{Platform: nodePlatform(), Arch: nodeArch(), Node: strings.TrimSpace(nodeResult.Stdout), NPM: strings.TrimSpace(npmResult.Stdout)}
	artifact, err := prepareArtifact(target, tempRoot, cwd)
	if err != nil {
		return model.VerificationResult{}, err
	}
	receipts := []model.RunReceipt{}
	incomplete := []model.IncompleteEvidence{}
	if config.Source != nil {
		receipt, evidence := runSource(config, environment)
		if receipt != nil {
			receipts = append(receipts, *receipt)
		}
		incomplete = append(incomplete, evidence...)
	}
	for _, profileName := range config.ProfileOrder {
		profile := config.Profiles[profileName]
		receipt, evidence := runProfile(profileName, profile.InstallArgs, artifact, config, environment, tempRoot)
		if receipt != nil {
			receipts = append(receipts, *receipt)
		}
		incomplete = append(incomplete, evidence...)
	}
	return model.VerificationResult{
		Artifact:   artifact.ArtifactReceipt,
		Receipts:   receipts,
		Incomplete: incomplete,
		Comparison: compare.Receipts(config, receipts),
	}, nil
}

func prepareArtifact(target, tempRoot, cwd string) (preparedArtifact, error) {
	artifactDir := filepath.Join(tempRoot, "artifact")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return preparedArtifact{}, err
	}
	resolvedTarget := target
	if strings.HasPrefix(target, ".") || filepath.IsAbs(target) {
		var err error
		resolvedTarget, err = filepath.Abs(target)
		if err != nil {
			return preparedArtifact{}, err
		}
	}
	result := processrun.Run("npm", []string{"pack", resolvedTarget, "--json", "--pack-destination", artifactDir}, cwd, 120*time.Second)
	if result.ExitCode == nil || *result.ExitCode != 0 || result.TimedOut || result.SpawnError != "" {
		return preparedArtifact{}, fmt.Errorf("npm pack failed: %s", sanitizeDiagnostic(commandDetail(result), tempRoot))
	}
	var entries []struct {
		Filename string `json:"filename"`
		Name     string `json:"name"`
		Version  string `json:"version"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &entries); err != nil {
		value := result.Stdout
		if len(value) > 400 {
			value = value[:400]
		}
		return preparedArtifact{}, fmt.Errorf("npm pack did not return valid JSON: %s", value)
	}
	if len(entries) != 1 {
		return preparedArtifact{}, fmt.Errorf("npm pack must return exactly one artifact")
	}
	entry := entries[0]
	if entry.Filename == "" || entry.Name == "" || entry.Version == "" {
		return preparedArtifact{}, fmt.Errorf("npm pack JSON is missing filename, name, or version")
	}
	tarballPath := filepath.Join(artifactDir, entry.Filename)
	info, err := os.Stat(tarballPath)
	if err != nil {
		return preparedArtifact{}, err
	}
	digest, err := canonical.SHA256File(tarballPath)
	if err != nil {
		return preparedArtifact{}, err
	}
	return preparedArtifact{ArtifactReceipt: model.ArtifactReceipt{PackageName: entry.Name, PackageVersion: entry.Version, Filename: entry.Filename, SHA256: digest, Size: info.Size()}, TarballPath: tarballPath}, nil
}

func runSource(config model.Contract, environment model.EnvironmentReceipt) (*model.RunReceipt, []model.IncompleteEvidence) {
	source := config.Source
	cwd, err := filepath.Abs(source.CWD)
	if err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: "source", Stage: "probe", Message: err.Error()}}
	}
	realCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		realCWD = cwd
	}
	runtimeIdentity, err := runtimeidentity.Create(model.RuntimeIdentity{
		Kind: "source", BinName: filepath.Base(source.Command), ExecutablePath: source.Command, ExecutableRealPath: source.Command,
		OptionalDependencies: []model.OptionalDependencyReceipt{},
	})
	if err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: "source", Stage: "probe", Message: err.Error()}}
	}
	incomplete := []model.IncompleteEvidence{}
	probes := runProbes(source.Command, source.Args, cwd, config, "source", &incomplete, []string{cwd, realCWD})
	return &model.RunReceipt{Subject: "source", Profile: "source", InstallArgs: []string{}, Environment: environment, Runtime: runtimeIdentity, Probes: probes}, incomplete
}

func runProfile(profileName string, installArgs []string, artifact preparedArtifact, config model.Contract, environment model.EnvironmentReceipt, tempRoot string) (*model.RunReceipt, []model.IncompleteEvidence) {
	incomplete := []model.IncompleteEvidence{}
	projectDir := filepath.Join(tempRoot, "profile-"+safeName(profileName))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "install", Message: err.Error()}}
	}
	manifestBytes, _ := json.Marshal(map[string]any{"name": "theseus-consumer-" + safeName(profileName), "private": true})
	if err := os.WriteFile(filepath.Join(projectDir, "package.json"), manifestBytes, 0o644); err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "install", Message: err.Error()}}
	}
	args := []string{"install", "--no-audit", "--no-fund", "--loglevel=error"}
	args = append(args, installArgs...)
	args = append(args, artifact.TarballPath)
	install := processrun.Run("npm", args, projectDir, 180*time.Second)
	if install.ExitCode == nil || *install.ExitCode != 0 || install.TimedOut || install.SpawnError != "" {
		message := fmt.Sprintf("npm install exited %s", displayExit(install.ExitCode))
		if install.TimedOut {
			message = "npm install timed out"
		} else if install.SpawnError != "" {
			message = install.SpawnError
		}
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "install", Message: message, Stderr: sanitizeDiagnostic(install.Stderr, tempRoot)}}
	}
	packageRoot := packageDirectory(filepath.Join(projectDir, "node_modules"), artifact.PackageName)
	manifest, err := readManifest(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "resolve-bin", Message: "installed package.json could not be read: " + err.Error()}}
	}
	binName, err := resolveBin(manifest, artifact.PackageName, config.Bin)
	if err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "resolve-bin", Message: err.Error()}}
	}
	binFile := binName
	if runtime.GOOS == "windows" {
		binFile += ".cmd"
	}
	executable := filepath.Join(projectDir, "node_modules", ".bin", binFile)
	realExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "resolve-bin", Message: fmt.Sprintf("bin %s is missing: %s", binName, err)}}
	}
	optionalDependencies := installedOptionalDependencies(projectDir, manifest)
	projectRealPath, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		projectRealPath = projectDir
	}
	executablePath, _ := filepath.Rel(projectDir, executable)
	executableRealPath, _ := filepath.Rel(projectRealPath, realExecutable)
	executablePath = filepath.ToSlash(executablePath)
	executableRealPath = filepath.ToSlash(executableRealPath)
	runtimeIdentity, err := runtimeidentity.Create(model.RuntimeIdentity{
		Kind: "installed", PackageName: artifact.PackageName, PackageVersion: artifact.PackageVersion, BinName: binName,
		ExecutablePath: executablePath, ExecutableRealPath: executableRealPath, OptionalDependencies: optionalDependencies,
	})
	if err != nil {
		return nil, []model.IncompleteEvidence{{Code: "THS-INCOMPLETE-001", Profile: profileName, Stage: "resolve-bin", Message: err.Error()}}
	}
	probes := runProbes(executable, nil, projectDir, config, profileName, &incomplete, []string{projectDir, projectRealPath})
	return &model.RunReceipt{Subject: "installed", Profile: profileName, InstallArgs: append([]string{}, installArgs...), Environment: environment, Runtime: runtimeIdentity, Probes: probes}, incomplete
}

func runProbes(command string, prefixArgs []string, cwd string, config model.Contract, profile string, incomplete *[]model.IncompleteEvidence, automaticRoots []string) []model.ProbeReceipt {
	receipts := []model.ProbeReceipt{}
	for _, probe := range config.Probes {
		args := append(append([]string{}, prefixArgs...), probe.Argv...)
		result := processrun.Run(command, args, cwd, time.Duration(probe.TimeoutMS)*time.Millisecond)
		stdout := normalize(result.Stdout, "stdout", probe.ID, config.Normalizers, automaticRoots)
		stderr := normalize(result.Stderr, "stderr", probe.ID, config.Normalizers, automaticRoots)
		receipts = append(receipts, model.ProbeReceipt{ID: probe.ID, Argv: append([]string{}, probe.Argv...), ExitCode: result.ExitCode, Signal: result.Signal, TimedOut: result.TimedOut, Stdout: stdout, Stderr: stderr, StdoutSHA256: canonical.SHA256String(stdout), StderrSHA256: canonical.SHA256String(stderr), DurationMS: result.DurationMS})
		if result.TimedOut || result.SpawnError != "" {
			message := result.SpawnError
			if result.TimedOut {
				message = fmt.Sprintf("probe timed out after %dms", probe.TimeoutMS)
				if result.SpawnError != "" {
					message += "; cleanup error: " + result.SpawnError
				}
			}
			evidence := model.IncompleteEvidence{Code: "THS-INCOMPLETE-001", Profile: profile, Stage: "probe", Probe: probe.ID, Message: message}
			if stderr != "" {
				evidence.Stderr = stderr
			}
			*incomplete = append(*incomplete, evidence)
		}
	}
	return receipts
}

func normalize(value, field, probe string, configs []model.NormalizerConfig, automaticRoots []string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	roots := append([]string{}, automaticRoots...)
	sort.Slice(roots, func(left, right int) bool { return len(roots[left]) > len(roots[right]) })
	for _, root := range roots {
		value = strings.ReplaceAll(value, root, "<subject-root>")
	}
	for _, config := range configs {
		if config.Probe != "" && config.Probe != probe {
			continue
		}
		if !contains(config.Fields, field) {
			continue
		}
		prefix, _ := contract.RegexPrefix(config.Flags)
		expression := regexp.MustCompile(prefix + config.Pattern)
		if contract.HasGlobal(config.Flags) {
			value = expression.ReplaceAllString(value, config.Replacement)
		} else if match := expression.FindStringSubmatchIndex(value); match != nil {
			replacement := expression.ExpandString(nil, config.Replacement, value, match)
			value = value[:match[0]] + string(replacement) + value[match[1]:]
		}
	}
	return value
}

func sanitizeDiagnostic(value, tempRoot string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	roots := []string{tempRoot}
	if real, err := filepath.EvalSymlinks(tempRoot); err == nil {
		roots = append(roots, real)
	}
	sort.Slice(roots, func(left, right int) bool { return len(roots[left]) > len(roots[right]) })
	for _, root := range roots {
		value = strings.ReplaceAll(value, root, "<theseus-temp>")
	}
	unixLogs := regexp.MustCompile(`(?:/[^\s]+)?/\.npm/_logs/[^\s]+`)
	value = unixLogs.ReplaceAllString(value, "<npm-log>")
	windowsLogs := regexp.MustCompile(`(?i)(?:[A-Z]:\\|\\\\)[^\r\n]*?\\(?:npm-cache|\.npm)\\_logs\\[^\s]+`)
	return windowsLogs.ReplaceAllString(value, "<npm-log>")
}

func readManifest(path string) (packageManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return packageManifest{}, err
	}
	var manifest packageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return packageManifest{}, err
	}
	return manifest, nil
}

func resolveBin(manifest packageManifest, packageName, requested string) (string, error) {
	entries := map[string]string{}
	if len(manifest.Bin) > 0 && string(manifest.Bin) != "null" {
		var single string
		if err := json.Unmarshal(manifest.Bin, &single); err == nil {
			parts := strings.Split(packageName, "/")
			entries[parts[len(parts)-1]] = single
		} else {
			var values map[string]any
			if err := json.Unmarshal(manifest.Bin, &values); err == nil {
				for name, value := range values {
					if path, ok := value.(string); ok {
						entries[name] = path
					}
				}
			}
		}
	}
	if requested != "" {
		if _, exists := entries[requested]; !exists {
			return "", fmt.Errorf("requested bin %s is not declared by %s", requested, packageName)
		}
		return requested, nil
	}
	if len(entries) != 1 {
		return "", fmt.Errorf("%s declares %d bins; contract.bin is required", packageName, len(entries))
	}
	for name := range entries {
		return name, nil
	}
	return "", fmt.Errorf("internal: bin resolution found no entries for %s", packageName)
}

func installedOptionalDependencies(projectDir string, manifest packageManifest) []model.OptionalDependencyReceipt {
	names := []string{}
	for name := range manifest.OptionalDependencies {
		names = append(names, name)
	}
	sort.Strings(names)
	result := []model.OptionalDependencyReceipt{}
	for _, name := range names {
		dependency, err := readManifest(filepath.Join(packageDirectory(filepath.Join(projectDir, "node_modules"), name), "package.json"))
		if err == nil && dependency.Version != "" {
			result = append(result, model.OptionalDependencyReceipt{Name: name, Version: dependency.Version})
		}
	}
	return result
}

func packageDirectory(nodeModules, name string) string {
	parts := strings.Split(name, "/")
	return filepath.Join(append([]string{nodeModules}, parts...)...)
}
func safeName(value string) string {
	return regexp.MustCompile(`[^A-Za-z0-9_-]`).ReplaceAllString(value, "-")
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func nodePlatform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}
func nodeArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH
	}
}
func displayExit(value *int) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprint(*value)
}
func commandDetail(result processrun.Result) string {
	if result.SpawnError != "" {
		return result.SpawnError
	}
	if strings.TrimSpace(result.Stderr) != "" {
		return strings.TrimSpace(result.Stderr)
	}
	return "exit " + displayExit(result.ExitCode)
}
