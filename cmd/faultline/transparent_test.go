package main

import (
	"strings"
	"testing"
)

// The flag has to say what it needs, because everything transparent mode asks
// for - Linux, NET_ADMIN, a user of its own - is invisible until it fails.
func TestTransparentFlagSaysWhatItNeeds(t *testing.T) {
	flag := newServeCmd().Flags().Lookup("transparent")
	if flag == nil {
		t.Fatal("serve has no --transparent flag")
	}
	for _, want := range []string{"NET_ADMIN", "Linux", "iptables"} {
		if !strings.Contains(flag.Usage, want) {
			t.Errorf("the help does not mention %s: %s", want, flag.Usage)
		}
	}
}
