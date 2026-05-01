package extract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func init() {
	Register(".go", extractGo)
}

func extractGo(path string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	lines := splitLines(src)
	var out []Finding

	addIdent := func(name string, pos token.Pos) {
		p := fset.Position(pos)
		for _, sub := range Split(name) {
			out = append(out, Finding{
				File:    path,
				Line:    p.Line,
				Col:     p.Column,
				Token:   sub,
				Kind:    "ident",
				Snippet: snippet(lines, p.Line, 3),
				Ctx:     normCtx(lines, p.Line, 2),
			})
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Name != nil {
				addIdent(x.Name.Name, x.Name.Pos())
			}
		case *ast.TypeSpec:
			if x.Name != nil {
				addIdent(x.Name.Name, x.Name.Pos())
			}
		case *ast.ValueSpec:
			for _, n := range x.Names {
				addIdent(n.Name, n.Pos())
			}
		case *ast.Field:
			for _, n := range x.Names {
				addIdent(n.Name, n.Pos())
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						addIdent(id.Name, id.Pos())
					}
				}
			}
		}
		return true
	})

	for _, cg := range f.Comments {
		for _, c := range cg.List {
			text := stripCommentMarkers(c.Text)
			p := fset.Position(c.Pos())
			for _, w := range words(text) {
				out = append(out, Finding{
					File:    path,
					Line:    p.Line,
					Col:     p.Column,
					Token:   strings.ToLower(w),
					Kind:    "comment",
					Snippet: snippet(lines, p.Line, 2),
					Ctx:     normCtx(lines, p.Line, 1),
				})
			}
		}
	}
	return out, nil
}

func stripCommentMarkers(s string) string {
	s = strings.TrimPrefix(s, "//")
	s = strings.TrimPrefix(s, "/*")
	s = strings.TrimSuffix(s, "*/")
	return s
}
