package logging

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestContextAwareLoggerMethods(t *testing.T) {
	root := repositoryRoot(t)
	internalRoot := filepath.Join(root, "internal")
	var violations []string

	err := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		contextAliases, httpAliases := standardLibraryAliases(file)
		if len(contextAliases) == 0 && len(httpAliases) == 0 {
			return nil
		}

		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || !acceptsRequestContext(function.Type.Params, contextAliases, httpAliases) {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !isNonContextLoggerCall(call) {
					return true
				}
				position := fileSet.Position(call.Pos())
				relativePath, err := filepath.Rel(root, position.Filename)
				if err != nil {
					relativePath = position.Filename
				}
				violations = append(violations, relativePath+":"+strconv.Itoa(position.Line))
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan context-aware logger usage: %v", err)
	}
	if len(violations) == 0 {
		return
	}

	sort.Strings(violations)
	t.Fatalf(
		"request or worker functions with context must use DebugContext/InfoContext/WarnContext/ErrorContext:\n%s",
		strings.Join(violations, "\n"),
	)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root containing go.mod not found")
		}
		dir = parent
	}
}

func standardLibraryAliases(file *ast.File) (map[string]struct{}, map[string]struct{}) {
	contextAliases := make(map[string]struct{})
	httpAliases := make(map[string]struct{})
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		alias := filepath.Base(importPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		switch importPath {
		case "context":
			contextAliases[alias] = struct{}{}
		case "net/http":
			httpAliases[alias] = struct{}{}
		}
	}
	return contextAliases, httpAliases
}

func acceptsRequestContext(
	parameters *ast.FieldList,
	contextAliases map[string]struct{},
	httpAliases map[string]struct{},
) bool {
	if parameters == nil {
		return false
	}
	for _, parameter := range parameters.List {
		switch parameterType := parameter.Type.(type) {
		case *ast.SelectorExpr:
			if packageName, ok := parameterType.X.(*ast.Ident); ok &&
				parameterType.Sel.Name == "Context" &&
				containsAlias(contextAliases, packageName.Name) {
				return true
			}
		case *ast.StarExpr:
			requestType, ok := parameterType.X.(*ast.SelectorExpr)
			if ok && requestType.Sel.Name == "Request" {
				if packageName, ok := requestType.X.(*ast.Ident); ok && containsAlias(httpAliases, packageName.Name) {
					return true
				}
			}
		}
	}
	return false
}

func containsAlias(aliases map[string]struct{}, alias string) bool {
	_, ok := aliases[alias]
	return ok
}

func isNonContextLoggerCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch selector.Sel.Name {
	case "Debug", "Info", "Warn", "Error":
		return isLoggerExpression(selector.X)
	default:
		return false
	}
}

func isLoggerExpression(expression ast.Expr) bool {
	switch expression := expression.(type) {
	case *ast.Ident:
		return strings.HasSuffix(strings.ToLower(expression.Name), "logger")
	case *ast.SelectorExpr:
		return strings.HasSuffix(strings.ToLower(expression.Sel.Name), "logger") || isLoggerExpression(expression.X)
	case *ast.CallExpr:
		return isLoggerExpression(expression.Fun)
	case *ast.IndexExpr:
		return isLoggerExpression(expression.X)
	case *ast.IndexListExpr:
		return isLoggerExpression(expression.X)
	case *ast.ParenExpr:
		return isLoggerExpression(expression.X)
	default:
		return false
	}
}
