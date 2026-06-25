package main

import (
	"os"

	"github.com/mrfoh/duckmigrate/cli"
)

func main() {
	os.Exit(cli.Execute())
}
