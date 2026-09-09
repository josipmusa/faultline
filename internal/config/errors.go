package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Error is one problem with a configuration file, reported where it is written.
// Everything a user has to fix is in it: the file, the line, the path to the
// field, and what is wrong with it.
//
// Loading stops at the first Error. A configuration file is read top to bottom
// and the second complaint is usually a consequence of the first.
type Error struct {
	File    string
	Line    int
	Path    string // like rules[0].fault.ms; empty when the file itself is the problem
	Message string
}

func (e *Error) Error() string {
	where := e.File + ":" + strconv.Itoa(e.Line)
	if e.Path == "" {
		return where + ": " + e.Message
	}
	return where + ": " + e.Path + ": " + e.Message
}

// syntaxError turns a parser error into an Error. The YAML parser reports the
// line in its message rather than in a type, so the line is lifted out of the
// text and the rest of the sentence is kept as it is.
func syntaxError(file string, err error) *Error {
	text := strings.TrimPrefix(err.Error(), "yaml: ")
	line := 1

	if rest, ok := strings.CutPrefix(text, "line "); ok {
		if number, message, found := strings.Cut(rest, ": "); found {
			if n, convErr := strconv.Atoi(number); convErr == nil {
				line, text = n, message
			}
		}
	}
	return &Error{File: file, Line: line, Message: fmt.Sprintf("this is not valid YAML: %s", text)}
}
