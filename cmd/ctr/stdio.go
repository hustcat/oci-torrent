package main

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
)

type stdio struct {
	stdin  string
	stdout string
	stderr string
}

func createStdio() (s stdio, err error) {
	tmp, err := ioutil.TempDir("", "ctr-")
	if err != nil {
		return s, err
	}
	// create fifo's for the process
	for name, fd := range map[string]*string{
		"stdin":  &s.stdin,
		"stdout": &s.stdout,
		"stderr": &s.stderr,
	} {
		path := filepath.Join(tmp, name)
		if err := syscall.Mkfifo(path, 0755); err != nil && !os.IsExist(err) {
			return s, err
		}
		*fd = path
	}
	return s, nil
}

func attachStdio(s stdio) error {
	stdoutf, err := os.OpenFile(s.stdout, syscall.O_RDWR, 0)
	if err != nil {
		return err
	}
	go io.Copy(os.Stdout, stdoutf)
	stderrf, err := os.OpenFile(s.stderr, syscall.O_RDWR, 0)
	if err != nil {
		return err
	}
	go io.Copy(os.Stderr, stderrf)
	return nil
}
