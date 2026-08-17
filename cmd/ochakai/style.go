// Weight for the eye, and only for an eye (design doc 0111). A listing
// line carries an address, a couple of attributes and then prose, and
// with every field the same weight the prose runs into the row below it.
// Bold and dim separate them without adding a field or a word.
package main

import "os"

// styled is whether the human-facing printers may emit escapes. It is set
// once, in main, from stdout — so nothing reaches a pipe, and a test that
// does not ask for styling gets the same bytes it always got.
var styled bool

// Weight, not colour. Bold and dim are the two attributes every terminal
// worth the name renders, they mean the same thing to a reader whatever
// the theme, and neither carries information of its own: strip them and
// the line still says everything it said (design doc 0094's rule, that
// what is only said in a rendering is not said).
const (
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiReset = "\x1b[0m"
)

// bold marks the address — the field a reader's eye lands on to find the
// row, and the one they copy into the next command.
func bold(s string) string { return styleWith(ansiBold, s) }

// dim marks the prose that trails a row: the description and the passage
// saying why a hit matched. It is what the row has to say once you have
// found it, not what you scan the column for.
func dim(s string) string { return styleWith(ansiDim, s) }

func styleWith(seq, s string) string {
	if !styled || s == "" {
		return s
	}
	return seq + s + ansiReset
}

// terminalStyling reports whether f is a terminal being read by a person.
//
// A character device is the whole test: a pipe, a file and a captured
// buffer are all not one, so `ochakai search q | cut -f1` and
// `> hits.txt` get the bytes they got before this existed. NO_COLOR is
// the escape hatch for the person who is at a terminal and does not want
// it — the cross-program convention, honoured on presence rather than on
// value, which is what every other program that reads it does.
func terminalStyling(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
