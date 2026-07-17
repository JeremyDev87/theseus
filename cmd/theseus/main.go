package main

import (
	"os"

	"github.com/JeremyDev87/theseus/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
