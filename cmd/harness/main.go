package main

import (
	"fmt"
	"os"

	"github.com/fikrilal/agy-auth/internal/harness"
)

func main() {
	app, err := harness.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "harness: %v\n", err)
		os.Exit(1)
	}
	os.Exit(app.Run(os.Args[1:]))
}
