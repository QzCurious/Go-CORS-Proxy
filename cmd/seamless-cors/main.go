package main

import (
	"os"

	"github.com/QzCurious/seamless-cors/internal/inbound/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}
