package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

var operators = map[string]token.Token{
	"==": token.EQL,
	"!=": token.NEQ,
}

type Visitor struct {
	Token token.Token
	Exps  []*ast.BinaryExpr
}

func (v *Visitor) Visit(node ast.Node) ast.Visitor {
	if exp, ok := node.(*ast.BinaryExpr); ok {
		if exp.Op == v.Token {
			v.Exps = append(v.Exps, exp)
		}
	}
	return v
}

func Err(s string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+s, args...)
}

func Errf(s string, args ...interface{}) {
	Err(s, args...)
	os.Exit(1)
}

func main() {
	op := flag.String("op", "==", "operator to look for")
	rep := flag.String("rep", "!=", "replacement operator")
	outdir := flag.String("o", ".", "output directory")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: mutator [flags] [package]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if _, ok := operators[*op]; !ok {
		Errf("%s is not a valid mutator\n", *op)
	}
	if _, ok := operators[*rep]; !ok {
		Errf("%s is not a valid replacement\n", *rep)
	}

	pkgPath := flag.Arg(0)
	if pkgPath == "" {
		flag.Usage()
		Errf("must provide a package\n")
	}

	if err := MutatePackage(pkgPath, *op, *rep, *outdir); err != nil {
		Errf("%s\n", err)
	}
}

func MutatePackage(name, op, rep, out string) error {
	pkg, err := build.Import(name, "", 0)
	if err != nil {
		return fmt.Errorf("could not import %s: %s", name, err)
	}

	tmpDir, err := ioutil.TempDir("", "mutate")
	if err != nil {
		return fmt.Errorf("could not create temporary directory: %s", err)
	}

	fmt.Fprintf(os.Stderr, "using %s as a temporary directory\n", tmpDir)
	if err := copyDir(pkg.Dir, tmpDir); err != nil {
		return fmt.Errorf("could not copy package directory: %s", err)
	}

	for _, f := range pkg.GoFiles {
		srcFile := filepath.Join(tmpDir, f)
		if err := MutateFile(srcFile, op, rep); err != nil {
			return err
		}
	}
	return nil
}

func MutateFile(srcFile, op, rep string) error {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, srcFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("could not parse %s: %s", srcFile, err)
	}

	visitor := Visitor{Token: operators[op]}
	ast.Walk(&visitor, file)

	filename := filepath.Base(srcFile)
	fmt.Fprintf(os.Stderr, "%s has %d occurrences of %s\n", filename, len(visitor.Exps), op)
	for i, exp := range visitor.Exps {
		err := func() error {
			oldOp := exp.Op
			exp.Op = operators[rep]
			defer func() {
				exp.Op = oldOp
			}()

			if err := printAST(srcFile, fset, file); err != nil {
				return err
			}

			cmd := exec.Command("go", "test")
			cmd.Dir = filepath.Dir(srcFile)
			output, err := cmd.CombinedOutput()
			if err == nil {
				fmt.Fprintf(os.Stderr, "mutation %d did not fail tests\n", i)
			} else if _, ok := err.(*exec.ExitError); ok {
				lines := bytes.Split(output, []byte("\n"))
				lastLine := lines[len(lines)-2]
				if !bytes.HasPrefix(lastLine, []byte("FAIL")) {
					fmt.Fprintf(os.Stderr, "mutation %d tests resulted in an error: %s\n", i, lastLine)
				} else {
					fmt.Fprintf(os.Stderr, "mutation %d tests failed as expected\n", i)
				}
			} else {
				return fmt.Errorf("mutation %d failed to run tests: %s\n", i, err)
			}
			return nil
		}()
		if err != nil {
			return err
		}
	}

	if err := printAST(srcFile, fset, file); err != nil {
		return err
	}
	return nil
}

func printAST(path string, fset *token.FileSet, node interface{}) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return fmt.Errorf("could not create file: %s", err)
	}
	defer out.Close()

	if err := printer.Fprint(out, fset, node); err != nil {
		return fmt.Errorf("could not print %s: %s", path, err)
	}
	return nil
}
