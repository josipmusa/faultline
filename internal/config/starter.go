package config

import _ "embed"

// Starter is the commented faultline.yaml that `faultline init` writes into a
// directory that has none. It lives here rather than next to the command
// because this package decides what a configuration file may say, and the file
// a stranger meets first is the one that has to load without a word of
// explanation.
//
// It is deliberately inert: the rule it shows is disabled and its scenario is
// not active, so starting Faultline straight after `init` changes nothing
// about the traffic of the application being wrapped.
//
//go:embed starter.yaml
var Starter string
