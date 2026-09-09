// Command faultline is an HTTP and HTTPS fault-injection proxy for development
// and testing.
package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// A wrapped child that failed is not Faultline failing: exit with its
		// code and leave the explaining to the child.
		var exit exitError
		if errors.As(err, &exit) {
			os.Exit(int(exit))
		}
		fmt.Fprintln(os.Stderr, "faultline:", err)
		os.Exit(1)
	}
}
