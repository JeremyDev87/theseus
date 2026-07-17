package process

type Result struct {
	ExitCode   *int
	Signal     *string
	Stdout     string
	Stderr     string
	TimedOut   bool
	DurationMS int64
	SpawnError string
}
