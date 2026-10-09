// SPDX-License-Identifier: BSD-3-Clause

package certissue

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Two rules this package promises, checked against its own source rather than
// trusted: the time is read only through a Clock (the system one lives in
// systemClock's methods), and the only goroutine is the one Client.Start
// starts.

// clockReads are the standard library calls that read the wall clock or
// start a timer on it.
var clockReads = map[string]map[string]bool{
	"time": {
		"Now": true, "Since": true, "Until": true, "After": true, "AfterFunc": true,
		"Tick": true, "NewTimer": true, "NewTicker": true, "Sleep": true,
	},
	"context": {
		"WithTimeout": true, "WithDeadline": true, "WithTimeoutCause": true, "WithDeadlineCause": true,
	},
}

// audit reports every clock read and go statement in f outside the places
// allowed for them, and counts the allowed ones.
func audit(fset *token.FileSet, f *ast.File) (bad []string, goStmts, clockUses int) {
	local := map[string]string{} // local package name -> import path
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		if clockReads[path] == nil {
			continue
		}
		name := path
		if im.Name != nil {
			name = im.Name.Name
		}
		if name == "." || name == "_" {
			bad = append(bad, fset.Position(im.Pos()).String()+": "+name+" import of "+path)
			continue
		}
		local[name] = path
	}
	for _, decl := range f.Decls {
		recv, fn := "", ""
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fn = fd.Name.Name
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				recv = receiverType(fd.Recv.List[0].Type)
			}
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GoStmt:
				if recv == "Client" && fn == "Start" {
					goStmts++
				} else {
					bad = append(bad, fset.Position(n.Pos()).String()+": go statement outside Client.Start")
				}
			case *ast.SelectorExpr:
				id, ok := n.X.(*ast.Ident)
				if !ok || !clockReads[local[id.Name]][n.Sel.Name] {
					return true
				}
				if recv == "systemClock" && local[id.Name] == "time" {
					clockUses++
				} else {
					bad = append(bad, fset.Position(n.Pos()).String()+": "+local[id.Name]+"."+n.Sel.Name+" outside systemClock")
				}
			}
			return true
		})
	}
	return bad, goStmts, clockUses
}

func receiverType(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		e = star.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func TestOnlyTheClockReadsTimeAndOnlyStartSpawns(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var bad []string
	checked, goStmts, clockUses := 0, 0, 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		b, g, c := audit(fset, f)
		bad = append(bad, b...)
		goStmts += g
		clockUses += c
		checked++
	}
	for _, b := range bad {
		t.Error(b)
	}
	// The counts prove the walk saw the code it is meant to police: exactly
	// one goroutine, in Start, and the system clock's two reads.
	if checked < 6 || goStmts != 1 || clockUses != 2 {
		t.Fatalf("checked %d files, found %d go statements and %d clock reads; want >=6, 1 and 2", checked, goStmts, clockUses)
	}
}

// The check above can fail: each of these sources breaks a rule, and each must
// be caught. The last two are allowed, and must not be.
func TestGuardCatchesViolations(t *testing.T) {
	cases := []struct {
		src     string
		wantBad int
	}{
		{`package p; func f() { go g() }`, 1},
		{`package p; func (c *Client) Restart() { go c.loop() }`, 1},
		{`package p; func (c Client) Start() { go func() {}() }`, 0},
		{`package p; import "time"; func f() { _ = time.Now() }`, 1},
		{`package p; import "time"; var now = time.Now`, 1},
		{`package p; import clock "time"; func f() { _ = clock.Since(clock.Time{}) }`, 1},
		{`package p; import . "time"; func f() { _ = Now() }`, 1},
		{`package p; import "context"; func f() { context.WithTimeout(nil, 0) }`, 1},
		{`package p; import "time"; func (c *Client) Start() { <-time.After(1) }`, 1},
		{`package p; import "context"; func (systemClock) Now() { context.WithDeadline(nil, x) }`, 1},
		{`package p; import "time"; func (systemClock) Now() time.Time { return time.Now() }`, 0},
		{`package p; import "time"; func f() time.Duration { return time.Second * time.Duration(3) }`, 0},
	}
	for _, tc := range cases {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "case.go", tc.src, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if bad, _, _ := audit(fset, f); len(bad) != tc.wantBad {
			t.Errorf("%s: flagged %d (%v), want %d", tc.src, len(bad), bad, tc.wantBad)
		}
	}
}
