package cli

import (
	"bytes"
	"testing"
)

func TestTableLinesUpWordsLeftAndNumbersRight(t *testing.T) {
	var b bytes.Buffer
	rows := [][]string{
		{"DISTRIBUTION", "STATE", "WAIT", ""},
		{"Debian", "stopped", "10 s", ""},
		{"Ubuntu-24.04", "running", "300 s", ""},
		// A name in Korean takes two cells a letter.
		{"우분투", "stopped", "90 s", "not read"},
	}
	if err := writeTable(&b, []bool{false, false, true, false}, rows); err != nil {
		t.Fatal(err)
	}
	want := "DISTRIBUTION  STATE     WAIT\n" +
		"Debian        stopped   10 s\n" +
		"Ubuntu-24.04  running  300 s\n" +
		"우분투        stopped   90 s  not read\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}
}
