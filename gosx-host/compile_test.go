package main

import (
	"bytes"
	"go/format"
	"os"
	"os/exec"
	"testing"
)

func TestGoSXGeneratedShotCard(t *testing.T) {
	compiler, err := exec.LookPath("gosx")
	if err != nil {
		if os.Getenv("STORYBOARD_REQUIRE_GOSX") == "1" {
			t.Fatal(err)
		}
		t.Skip("optional GoSX compiler not installed")
	}
	generated, err := exec.Command(compiler, "compile", "board.gsx").Output()
	if err != nil {
		t.Fatal(err)
	}
	checked, err := os.ReadFile("board_generated.go")
	if err != nil {
		t.Fatal(err)
	}
	_, checked, ok := bytes.Cut(checked, []byte("\n"))
	if !ok {
		t.Fatal("missing generated notice")
	}
	want, err := format.Source(generated)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := format.Source(checked)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, want) {
		t.Fatal("board_generated.go differs from GoSX compiler output")
	}
}
