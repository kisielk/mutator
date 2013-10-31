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
	"strings"
)

type mutation struct {
	op       token.Token
	category string
}

var operators = map[token.Token]mutation{
	// Comparisons
	token.EQL: {token.NEQ, "comparison"},
	token.LSS: {token.GEQ, "comparison"},
	token.GTR: {token.LEQ, "comparison"},
	token.NEQ: {token.EQL, "comparison"},
	token.LEQ: {token.GTR, "comparison"},
	token.GEQ: {token.LSS, "comparison"},

	// Logical
	token.LAND: {token.LOR, "logical"},
	token.LOR:  {token.LAND, "logical"},

	// Arithmetic
	token.ADD: {token.SUB, "arithmetic"},
	token.SUB: {token.ADD, "arithmetic"},
	token.MUL: {token.QUO, "arithmetic"},
	token.QUO: {token.MUL, "arithmetic"},

	// Binary
	token.AND: {token.OR, "binary"},
	token.OR:  {token.AND, "binary"},
	token.XOR: {token.AND, "binary"},
	token.SHL: {token.SHR, "binary"},
	token.SHR: {token.SHL, "binary"},
}

type BinaryExprVisitor struct {
	// Categories is a set of operator categories to consider for mutation
	Categories map[string]bool

	// Exps is a list of binary expressions discovered by the visitor
	Exps []*ast.BinaryExpr
}

func (v *BinaryExprVisitor) Visit(node ast.Node) ast.Visitor {
	if exp, ok := node.(*ast.BinaryExpr); ok {
		if _, ok := operators[exp.Op]; ok && v.Categories[operators[exp.Op].category] {
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
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: mutator [flags] [package] [testflags]\n")
		flag.PrintDefaults()
	}
	categories := flag.String("categories", "comparison,logical,arithmetic,binary",
		"A comma-separated list of mutation categories to enable. All categories are enabled by default.")
	flag.Parse()

	pkgPath := flag.Arg(0)
	if pkgPath == "" {
		flag.Usage()
		Errf("must provide a package\n")
	}

	var testFlags []string
	if flag.NArg() > 1 {
		testFlags = flag.Args()[1:]
	}

	enabledCategories := make(map[string]bool)
	for _, cat := range strings.Split(*categories, ",") {
		enabledCategories[cat] = true
	}

	if err := MutatePackage(pkgPath, testFlags, enabledCategories); err != nil {
		Errf("%s\n", err)
	}
}

func MutatePackage(name string, testFlags []string, enabledCategories map[string]bool) error {
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
		if err := MutateFile(srcFile, testFlags, enabledCategories); err != nil {
			return err
		}
	}
	return nil
}

func MutationID(pos token.Position) string {
	pos.Filename = filepath.Base(pos.Filename)
	return pos.String()
}

func MutateFile(srcFile string, testFlags []string, enabledCategories map[string]bool) error {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, srcFile, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("could not parse %s: %s", srcFile, err)
	}

	visitor := BinaryExprVisitor{Categories: enabledCategories}
	ast.Walk(&visitor, file)

	filename := filepath.Base(srcFile)
	fmt.Fprintf(os.Stderr, "%s has %d mutation sites\n", filename, len(visitor.Exps))
	for _, exp := range visitor.Exps {
		err := func() error {
			oldOp := exp.Op
			exp.Op = operators[exp.Op].op
			defer func() {
				exp.Op = oldOp
			}()

			if err := printAST(srcFile, fset, file); err != nil {
				return err
			}

			args := []string{"test"}
			args = append(args, testFlags...)
			cmd := exec.Command("go", args...)
			cmd.Dir = filepath.Dir(srcFile)
			output, err := cmd.CombinedOutput()
			if err == nil {
				fmt.Fprintf(os.Stderr, "mutation %s did not fail tests\n", MutationID(fset.Position(exp.OpPos)))
			} else if _, ok := err.(*exec.ExitError); ok {
				lines := bytes.Split(output, []byte("\n"))
				lastLine := lines[len(lines)-2]
				if !bytes.HasPrefix(lastLine, []byte("FAIL")) {
					fmt.Fprintf(os.Stderr, "mutation %s tests resulted in an error: %s\n", MutationID(fset.Position(exp.OpPos)), lastLine)
				} else {
					fmt.Fprintf(os.Stderr, "mutation %s tests failed as expected\n", MutationID(fset.Position(exp.OpPos)))
				}
			} else {
				return fmt.Errorf("mutation %s failed to run tests: %s\n", MutationID(fset.Position(exp.OpPos)), err)
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
