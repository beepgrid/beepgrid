// Command doctools extracts public Go declarations for the documentation drift check.
package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
)

func main() {
	fs := token.NewFileSet()
	packages, err := parser.ParseDir(fs, "sdk/go", func(i os.FileInfo) bool { return !strings.HasSuffix(i.Name(), "_test.go") }, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	out := []map[string]any{}
	text := func(n any) string {
		var b bytes.Buffer
		if err := format.Node(&b, fs, n); err != nil {
			panic(err)
		}
		return b.String()
	}
	for _, p := range packages {
		paths := make([]string, 0, len(p.Files))
		for path := range p.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			f := p.Files[path]
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					kind := "function"
					if d.Recv != nil {
						kind = "method"
					}
					parameters := []map[string]any{}
					for _, field := range d.Type.Params.List {
						for _, name := range field.Names {
							parameters = append(parameters, map[string]any{"name": name.Name, "type": text(field.Type)})
						}
					}
					out = append(out, map[string]any{"kind": kind, "name": d.Name.Name, "signature": text(d.Type), "parameters": parameters, "doc": d.Doc.Text(), "file": path})
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, s := range d.Specs {
						t := s.(*ast.TypeSpec)
						if !t.Name.IsExported() {
							continue
						}
						fields := []map[string]any{}
						if st, ok := t.Type.(*ast.StructType); ok {
							for _, field := range st.Fields.List {
								for _, name := range field.Names {
									if name.IsExported() {
										tag := ""
										if field.Tag != nil {
											tag = field.Tag.Value
										}
										fields = append(fields, map[string]any{"name": name.Name, "type": text(field.Type), "tag": tag, "doc": field.Doc.Text() + field.Comment.Text()})
									}
								}
							}
						}
						out = append(out, map[string]any{"kind": "type", "name": t.Name.Name, "definition": text(t.Type), "doc": d.Doc.Text(), "fields": fields, "file": path})
					}
				}
			}
		}
	}
	json.NewEncoder(os.Stdout).Encode(out)
}
