package main

import (
	"os"

	"wp/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
