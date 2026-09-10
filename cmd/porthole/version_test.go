package main

import "testing"

func TestVersionStringNonEmpty(t *testing.T) {
	if versionString() == "" {
		t.Fatal("empty version")
	}
}

func TestStripV(t *testing.T) {
	if stripV("v0.0.2") != "0.0.2" {
		t.Fatal(stripV("v0.0.2"))
	}
	if stripV("0.0.2") != "0.0.2" {
		t.Fatal(stripV("0.0.2"))
	}
}
