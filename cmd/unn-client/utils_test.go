package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestGetUniquePath(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "unn-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "test.txt")

	// 1. Path doesn't exist
	unique := getUniquePath(path)
	if unique != path {
		t.Errorf("expected %s, got %s", path, unique)
	}

	// 2. Path exists
	err = ioutil.WriteFile(path, []byte("hello"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	unique = getUniquePath(path)
	expected := filepath.Join(tmpDir, "test (1).txt")
	if unique != expected {
		t.Errorf("expected %s, got %s", expected, unique)
	}

	// 3. Path (1) also exists
	err = ioutil.WriteFile(expected, []byte("world"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	unique = getUniquePath(path)
	expected2 := filepath.Join(tmpDir, "test (2).txt")
	if unique != expected2 {
		t.Errorf("expected %s, got %s", expected2, unique)
	}
}
