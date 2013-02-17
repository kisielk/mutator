package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
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
		fmt.Fprintf(os.Stderr, "Usage: mutator [flags] [filename]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if _, ok := operators[*op]; !ok {
		Errf("%s is not a valid mutator\n", *op)
	}
	if _, ok := operators[*rep]; !ok {
		Errf("%s is not a valid replacement\n", *rep)
	}

	filename := flag.Arg(0)
	if filename == "" {
		flag.Usage()
		Errf("must provide a filename\n")
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.ParseComments)
	if err != nil {
		Errf("could not parse %s: %s\n", filename, err)
	}

	visitor := Visitor{Token: operators[*op]}
	ast.Walk(&visitor, file)

	fmt.Printf("You have %d occurences of %s\n", len(visitor.Exps), *op)
	for i, exp := range visitor.Exps {
		name := filepath.Base(filename)
		dir := filepath.Join(*outdir, fmt.Sprintf("%s_%d", name, i))
		if err := os.Mkdir(dir, 0770); err != nil {
			Err("could not create directory: %s\n", err)
			continue
		}
		path := filepath.Join(dir, name)
		fmt.Println(path)
		func() {
			out, err := os.Create(path)
			if err != nil {
				Err("could not create file: %s\n", err)
				return
			}
			defer out.Close()

			oldOp := exp.Op
			exp.Op = operators[*rep]
			printer.Fprint(out, fset, file)
			exp.Op = oldOp
		}()
	}
}
