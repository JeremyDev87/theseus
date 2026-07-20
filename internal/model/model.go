package model

type CompareField string

const (
	FieldExit    CompareField = "exit"
	FieldStdout  CompareField = "stdout"
	FieldStderr  CompareField = "stderr"
	FieldRuntime CompareField = "runtime"
)

type JSONValueType string

type ProfileConfig struct {
	InstallArgs []string `json:"installArgs"`
}

type SourceConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	CWD     string   `json:"cwd"`
}

type JSONPathExpectation struct {
	Path string        `json:"path"`
	Type JSONValueType `json:"type"`
}

type ProbeExpectation struct {
	Exit       *int                  `json:"exit,omitempty"`
	StdoutJSON []JSONPathExpectation `json:"stdoutJson,omitempty"`
}

type ProbeConfig struct {
	ID        string            `json:"id"`
	Argv      []string          `json:"argv"`
	TimeoutMS int               `json:"timeoutMs"`
	Compare   []CompareField    `json:"compare"`
	Expect    *ProbeExpectation `json:"expect,omitempty"`
}

type NormalizerConfig struct {
	ID          string   `json:"id"`
	Probe       string   `json:"probe,omitempty"`
	Fields      []string `json:"fields"`
	Pattern     string   `json:"pattern"`
	Flags       string   `json:"flags"`
	Replacement string   `json:"replacement"`
}

type AllowedDelta struct {
	Probe      string       `json:"probe"`
	Between    [2]string    `json:"between"`
	Field      CompareField `json:"field"`
	Reason     string       `json:"reason"`
	FromSet    bool         `json:"-"`
	From       *int         `json:"from,omitempty"`
	ToSet      bool         `json:"-"`
	To         *int         `json:"to,omitempty"`
	FromSHA256 string       `json:"fromSha256,omitempty"`
	ToSHA256   string       `json:"toSha256,omitempty"`
}

type Contract struct {
	Version       int                      `json:"version"`
	Bin           string                   `json:"bin,omitempty"`
	Source        *SourceConfig            `json:"source,omitempty"`
	Profiles      map[string]ProfileConfig `json:"profiles"`
	ProfileOrder  []string                 `json:"-"`
	Probes        []ProbeConfig            `json:"probes"`
	Normalizers   []NormalizerConfig       `json:"normalizers"`
	AllowedDeltas []AllowedDelta           `json:"allowedDeltas"`
}

type ArtifactReceipt struct {
	PackageName    string `json:"packageName"`
	PackageVersion string `json:"packageVersion"`
	Filename       string `json:"filename"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
}

type EnvironmentReceipt struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Node     string `json:"node"`
	NPM      string `json:"npm"`
}

type OptionalDependencyReceipt struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type RuntimeIdentity struct {
	Kind                 string                      `json:"kind"`
	PackageName          string                      `json:"packageName,omitempty"`
	PackageVersion       string                      `json:"packageVersion,omitempty"`
	BinName              string                      `json:"binName"`
	ExecutablePath       string                      `json:"executablePath"`
	ExecutableRealPath   string                      `json:"executableRealPath"`
	OptionalDependencies []OptionalDependencyReceipt `json:"optionalDependencies"`
	Canonical            string                      `json:"canonical"`
	SHA256               string                      `json:"sha256"`
}

type ProbeReceipt struct {
	ID           string   `json:"id"`
	Argv         []string `json:"argv"`
	ExitCode     *int     `json:"exitCode"`
	Signal       *string  `json:"signal"`
	TimedOut     bool     `json:"timedOut"`
	Stdout       string   `json:"stdout"`
	Stderr       string   `json:"stderr"`
	StdoutSHA256 string   `json:"stdoutSha256"`
	StderrSHA256 string   `json:"stderrSha256"`
	DurationMS   int64    `json:"durationMs"`
}

type RunReceipt struct {
	Subject     string             `json:"subject"`
	Profile     string             `json:"profile"`
	InstallArgs []string           `json:"installArgs"`
	Environment EnvironmentReceipt `json:"environment"`
	Runtime     RuntimeIdentity    `json:"runtime"`
	Probes      []ProbeReceipt     `json:"probes"`
}

type IncompleteEvidence struct {
	ID      string `json:"id"`
	Code    string `json:"code"`
	Profile string `json:"profile"`
	Stage   string `json:"stage"`
	Probe   string `json:"probe,omitempty"`
	Message string `json:"message"`
	Stderr  string `json:"stderr,omitempty"`
}

type Finding struct {
	ID       string            `json:"id"`
	Code     string            `json:"code"`
	Probe    string            `json:"probe"`
	Field    string            `json:"field"`
	Profiles []string          `json:"profiles"`
	Locator  string            `json:"locator"`
	Message  string            `json:"message"`
	Expected any               `json:"expected,omitempty"`
	Actual   any               `json:"actual,omitempty"`
	Digests  map[string]string `json:"digests,omitempty"`
}

type AllowedDifference struct {
	Probe    string       `json:"probe"`
	Field    CompareField `json:"field"`
	Profiles [2]string    `json:"profiles"`
	Reason   string       `json:"reason"`
}

type ComparisonResult struct {
	Findings           []Finding           `json:"findings"`
	AllowedDifferences []AllowedDifference `json:"allowedDifferences"`
}

type VerificationResult struct {
	Artifact   ArtifactReceipt      `json:"artifact"`
	Receipts   []RunReceipt         `json:"receipts"`
	Incomplete []IncompleteEvidence `json:"incomplete"`
	Comparison ComparisonResult     `json:"comparison"`
}

type ToolReceipt struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type VerificationReport struct {
	SchemaVersion   int                  `json:"schemaVersion"`
	IdentityVersion int                  `json:"identityVersion"`
	Tool            ToolReceipt          `json:"tool"`
	Target          string               `json:"target"`
	Status          string               `json:"status"`
	ExitCode        int                  `json:"exitCode"`
	Artifact        ArtifactReceipt      `json:"artifact"`
	Receipts        []RunReceipt         `json:"receipts"`
	Incomplete      []IncompleteEvidence `json:"incomplete"`
	Comparison      ComparisonResult     `json:"comparison"`
}
