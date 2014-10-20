package main

import (
	"bytes"
	"github.com/gopherjs/go-angularjs"
	"github.com/gopherjs/gopherjs/compiler"
	"github.com/gopherjs/gopherjs/js"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"strings"
	"time"
	"fmt"
)

type Line map[string]string

var output []Line

const initCode = `package main

import (
	"fmt"
	// "github.com/gopherjs/gopherjs/js"
)

func main() {
	fmt.Println("Hello, playground")
	// js.Global.Call("alert", "Hello, Javascript")
	// println("Hello, JS console")
}
`

func errLine(s string) Line {
	return Line {
		"type": "err",
		"content": s,
	}
}

func errErrLine(e error) Line {
	return errLine(e.Error())
}

func getCode() []byte {
	return []byte(js.Global.Call("getEditorValue").Str())
	// return []byte(scope.Get("code").Str())
}

func setCode(s string) {
	js.Global.Call("setEditorValue", s)
}

func control(scope *angularjs.Scope) {
	scope.Set("code", initCode)
	scope.Set("showGenerated", false)
	scope.Set("generated", `(generated code will be shown here after clicking "Run")`)

	packages := make(map[string]*compiler.Archive)
	var pkgsToLoad []string
	importContext := compiler.NewImportContext(
		func(path string) (*compiler.Archive, error) {
			if pkg, found := packages[path]; found {
				return pkg, nil
			}
			pkgsToLoad = append(pkgsToLoad, path)
			return &compiler.Archive{}, nil
		},
	)
	fileSet := token.NewFileSet()
	pkgsReceived := 0

	setupEnvironment(scope)

	var run func(bool)
	run = func(loadOnly bool) {
		output = nil
		scope.Set("output", output)
		pkgsToLoad = nil

		file, err := parser.ParseFile(fileSet, "prog.go",
			getCode(), parser.ParseComments,
		)
		if err != nil {
			if list, ok := err.(scanner.ErrorList); ok {
				for _, entry := range list {
					output = append(output, errErrLine(entry))
				}
				scope.Set("output", output)
				return
			}
			scope.Set("output", []Line{errErrLine(err)})
			return
		}

		mainPkg, err := compiler.Compile("main",
			[]*ast.File{file}, fileSet,
			importContext, false,
		)
		packages["main"] = mainPkg
		if err != nil && len(pkgsToLoad) == 0 {
			if list, ok := err.(compiler.ErrorList); ok {
				output := make([]Line, 0)
				for _, entry := range list {
					output = append(output, errErrLine(entry))
				}
				scope.Set("output", output)
				return
			}
			scope.Set("output", []Line{errErrLine(err)})
			return
		}

		var allPkgs []*compiler.Archive
		if len(pkgsToLoad) == 0 {
			for _, depPath := range mainPkg.Dependencies {
				dep, _ := importContext.Import(string(depPath))
				allPkgs = append(allPkgs, dep)
			}
			allPkgs = append(allPkgs, mainPkg)
		}

		if len(pkgsToLoad) != 0 {
			pkgsReceived = 0
			for _, p := range pkgsToLoad {
				path := p

				req := js.Global.Get("XMLHttpRequest").New()
				req.Call("open", "GET", "pkg/"+path+".a", true)
				req.Set("responseType", "arraybuffer")
				req.Set("onload", func() {
					if req.Get("status").Int() != 200 {

						f := func() {
							emsg := fmt.Sprintf("cannot load package \"%s\"", path)
							scope.Set("output", []Line{errLine(emsg)})
						}
						scope.Apply(f)
						return
					}

					data := js.Global.Get("Uint8Array").New(req.Get("response")).Interface().([]byte)
					packages[path], err = compiler.UnmarshalArchive(
						path+".a", path, []byte(data), importContext,
					)
					if err != nil {
						scope.Apply(func() {
							scope.Set("output", []Line{errErrLine(err)})
						})
						return
					}
					pkgsReceived++
					if pkgsReceived == len(pkgsToLoad) {
						run(loadOnly)
					}
				})
				req.Call("send")
			}
			return
		}

		if loadOnly {
			return
		}

		mainPkgCode := bytes.NewBuffer(nil)
		compiler.WritePkgCode(packages["main"], false,
			&compiler.SourceMapFilter{Writer: mainPkgCode},
		)
		scope.Set("generated", mainPkgCode.String())

		jsCode := bytes.NewBuffer(nil)
		jsCode.WriteString("try{\n")
		compiler.WriteProgramCode(allPkgs, importContext,
			&compiler.SourceMapFilter{Writer: jsCode},
		)
		jsCode.WriteString("} catch (err) {\ngoPanicHandler(err.message);\n}\n")
		js.Global.Call("eval", js.InternalObject(jsCode.String()))
	}

	scope.Set("run", run)
	run(true)

	scope.Set("format", func() {
		out, err := format.Source(getCode())
		if err != nil {
			scope.Set("output", []Line{errErrLine(err)})
			return
		}
		
		setCode(string(out))
		scope.Set("output", []Line{})
	})
}

func main() {
	app := angularjs.NewModule("playground", nil)

	app.NewController("PlaygroundCtrl", control)
}

func outLine(s string) Line {
	return Line {
		"type": "err",
		"content": s,
	}
}

func setupEnvironment(scope *angularjs.Scope) {
	js.Global.Set("goPrintToConsole", js.InternalObject(func(b []byte) {
		lines := strings.Split(string(b), "\n")
		if len(output) == 0 || output[len(output)-1]["type"] != "out" {
			output = append(output, Line{"type": "out", "content": ""})
		}
		output[len(output)-1]["content"] += lines[0]
		for i := 1; i < len(lines); i++ {
			output = append(output, outLine(lines[i]))
		}
		scope.Set("output", output)
		scope.EvalAsync(func() {
			time.AfterFunc(0, func() {
				box := angularjs.ElementById("output")
				box.SetProp("scrollTop", box.Prop("scrollHeight"))
			})
		})
	}))

	js.Global.Set("goPanicHandler", js.InternalObject(func(msg string) {
		output = append(output, errLine("panic:" + msg))
		scope.Set("output", output)
	}))
}
