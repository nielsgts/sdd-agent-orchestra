package main

import (
	"fmt"
	"log"
	"strings"
	"testing"
)

func TestToolInternalListDirEmpty(t *testing.T) {
	msg, err := toolInternalListDir(map[string]any{"baseDir": ".", "ignore": []any{".git"}}, AgentContext{})("{\"path\": \"\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalListDir()(path="") = %q, %v`, msg, err)
	}
}

func TestToolInternalListDirDot(t *testing.T) {
	msg, err := toolInternalListDir(map[string]any{"baseDir": "."}, AgentContext{})("{\"path\": \".\"}\n")
	if err != nil {
		t.Errorf(`toolInternalListDir()(path=".") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileDot(t *testing.T) {
	msg, err := toolInternalReadFile(".")("{\"path\": \"README.md\"}\n")
	if err != nil {
		t.Errorf(`toolInternalReadFile()(path="README.md") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileSrc(t *testing.T) {
	msg, err := toolInternalReadFile(".sdd")("{\"path\": \"models/default.json\"}\n")
	if err != nil {
		t.Errorf(`toolInternalReadFile()(path="models/default.json") = %q, %v`, msg, err)
	}
}

func TestToolInternalReadFileDotSrc(t *testing.T) {
	msg, err := toolInternalReadFile(".sdd")("{\"path\": \"../README.md\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalReadFile()(path="../README.md") = %q, %v`, msg, err)
	} else if !strings.HasPrefix(msg, "ERROR") {
		t.Errorf(`toolInternalReadFile()(path="../README.md") = %q`, msg)
	}
}

func TestToolInternalReadFileWin2Src(t *testing.T) {
	msg, err := toolInternalReadFile(".sdd")("{\"path\": \"specs\\\\design.md\"}\n")
	fmt.Println(msg)
	if err != nil {
		t.Errorf(`toolInternalReadFile()(path="specs\\\\design.md") = %q, %v`, msg, err)
	}
}

func TestToolInternalWriteFile(t *testing.T) {
	msg, err := toolInternalWriteFile(".", log.Default())("{\"path\": \"test.txt\", \"file_content\": \"TestToolInternalWriteFile\"}\n")
	fmt.Println(msg)
	if err != nil || strings.HasPrefix(msg, "ERROR") {
		t.Errorf(`toolInternalWriteFile()(path="test.txt") = %q, %v`, msg, err)
	}
}
