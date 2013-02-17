package main

import (
	"io"
	"os"
	"path/filepath"
)

// copyDir non-recursively copies the contents of the directory src to the directory dst
func copyDir(src, dst string) error {
	dir, err := os.Open(src)
	if err != nil {
		return err
	}

	contents, err := dir.Readdir(0)
	if err != nil {
		return err
	}

	for _, f := range contents {
		if f.IsDir() || f.Mode()&os.ModeType > 0 {
			continue
		}
		if err := copyFile(filepath.Join(src, f.Name()), dst); err != nil {
			return err
		}
	}

	return nil
}

// copyFile copies the file given by src to the directory dir
func copyFile(src, dir string) error {
	name := filepath.Base(src)
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
