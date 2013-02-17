package main

import (
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

	fset := token.NewFileSet()

	for i, f := range pkg.GoFiles {
		srcFile := filepath.Join(pkg.Dir, f)
		file, err := parser.ParseFile(fset, srcFile, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("could not parse %s: %s", srcFile, err)
		}

		visitor := Visitor{Token: operators[op]}
		ast.Walk(&visitor, file)

		fmt.Fprintf(os.Stderr, "%s has %d occurrences of %s\n", f, len(visitor.Exps), op)
		path := filepath.Join(tmpDir, filepath.Base(srcFile))
		for _, exp := range visitor.Exps {
			func() {
				oldOp := exp.Op
				exp.Op = operators[rep]
				defer func() {
					exp.Op = oldOp
				}()

				if err := printAST(path, fset, file); err != nil {
					Err("%s", err)
					return
				}

				cmd := exec.Command("go", "test")
				cmd.Dir = tmpDir
				_, err = cmd.CombinedOutput()
				if err == nil {
					fmt.Fprintf(os.Stderr, "mutation %d did not fail tests\n", i)
				}
			}()
		}
		if err := printAST(path, fset, file); err != nil {
			Errf("%s", err)
		}
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
