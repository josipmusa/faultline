// Command faultline is an HTTP and HTTPS fault-injection proxy for development
// and testing.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "faultline:", err)
		os.Exit(1)
	}
}
