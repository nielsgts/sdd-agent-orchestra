package main

import (
	"fmt"
	"log"
	"testing"
)

func TestToolInternalListDirEmpty(t *testing.T) {
	msg, err := toolInternalListDir("project")("{\"path\": \"\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalListDir()(path="") = %q, %v`, msg, err)
	}
}

func TestToolInternalListDirDot(t *testing.T) {
	msg, err := toolInternalListDir("project")("{\"path\": \".\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalListDir()(path=".") = %q, %v`, msg, err)
	}
}

func TestToolInternalListDirSrc(t *testing.T) {
	msg, err := toolInternalListDir("project")("{\"path\": \"src\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalListDir()(path="src") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileDot(t *testing.T) {
	msg, err := toolInternalReadFile("project")("{\"path\": \"test.txt\"}\n")
	fmt.Println(msg)
	if err != nil || msg != "test" {
		t.Errorf(`toolInternalReadFile()(path="test.txt") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileSrc(t *testing.T) {
	msg, err := toolInternalReadFile("project")("{\"path\": \"src/test.src\"}\n")
	fmt.Println(msg)
	if err != nil || msg != "src" {
		t.Errorf(`toolInternalReadFile()(path="src/test.src") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileDotSrc(t *testing.T) {
	msg, err := toolInternalReadFile("project")("{\"path\": \"./src/test.src\"}\n")
	fmt.Println(msg)
	if err != nil || msg != "src" {
		t.Errorf(`toolInternalReadFile()(path="./src/test.src") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileWin2Src(t *testing.T) {
	msg, err := toolInternalReadFile("project")("{\"path\": \"src\\\\test.src\"}\n")
	fmt.Println(msg)
	if err != nil || msg != "src" {
		t.Errorf(`toolInternalReadFile()(path="src\\\\test.src") = %q, %v`, msg, err)
	}
}

func TestToolInternalWriteFile(t *testing.T) {
	msg, err := toolInternalWriteFile("project", log.Default())("{\"path\": \"test.txt\", \"content\": \"TestToolInternalWriteFile\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalWriteFile()(path="test.txt") = %q, %v`, msg, err)
	}
}
