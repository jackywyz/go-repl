package main

import (
	"bufio";
	"bytes";
	"container/vector";
	"fmt";
	"go/ast";
	"go/parser";
	"go/printer";
	"io";
	"io/ioutil";
	"os";
	"strings";
)

type World struct {
	pkgs	*vector.StringVector;
	defs	*vector.StringVector;
	code	*vector.Vector;
	exec	string;
}

const TEMPPATH = "/tmp/gorepl"


var (
	bin	= os.Getenv("GOBIN");
	arch	= map[string]string{
		"amd64": "6",
		"i386": "8",
		"arm": "5",
	}[os.Getenv("GOARCH")];
)

func (self *World) source() string {
	source := "package main\n";

	for _, v := range self.pkgs.Data() {
		source += "import \"" + v + "\"\n"
	}

	source += "\n";

	for _, d := range self.defs.Data() {
		source += d + "\n\n"
	}

	source += "func noop(_ interface{}) {}\n\n";

	source += "func main() {\n";

	for _, c := range self.code.Data() {
		str := new(bytes.Buffer);
		printer.Fprint(str, c);

		source += "\t" + str.String() + ";\n";
		switch c.(type) {
		case *ast.AssignStmt:
			for _, name := range c.(*ast.AssignStmt).Lhs {
				str := new(bytes.Buffer);
				printer.Fprint(str, name);
				source += "\t" + "noop(" + str.String() + ");\n";
			}
		}
	}

	if self.exec != "" {
		source += "\t" + self.exec + ";\n"
	}

	source += "}\n";

	return source;
}

func compile(w *World) *bytes.Buffer {
	ioutil.WriteFile(TEMPPATH+".go", strings.Bytes(w.source()), 0644);

	err := new(bytes.Buffer);

	re, e, _ := os.Pipe();

	os.ForkExec(
		bin+"/"+arch+"g",
		[]string{bin + "/" + arch + "g", "-o", TEMPPATH + ".6", TEMPPATH + ".go"},
		os.Environ(),
		"",
		[]*os.File{nil, e, nil});

	e.Close();
	io.Copy(err, re);

	if err.Len() > 0 {
		return err
	}

	re, e, _ = os.Pipe();
	os.ForkExec(
		bin+"/"+arch+"l",
		[]string{bin + "/" + arch + "l", "-o", TEMPPATH + "", TEMPPATH + ".6"},
		os.Environ(),
		"",
		[]*os.File{nil, e, nil});

	e.Close();
	io.Copy(err, re);

	return err;
}

func run() (*bytes.Buffer, *bytes.Buffer) {
	out := new(bytes.Buffer);
	err := new(bytes.Buffer);

	re, e, _ := os.Pipe();
	ro, o, _ := os.Pipe();
	os.ForkExec(
		TEMPPATH,
		[]string{TEMPPATH},
		os.Environ(),
		"",
		[]*os.File{nil, o, e});

	e.Close();
	io.Copy(err, re);

	if err.Len() > 0 {
		return nil, err
	}

	o.Close();
	io.Copy(out, ro);

	return out, err;
}

func main() {
	fmt.Println("Welcome to the Go REPL!");
	fmt.Println("Enter '?' for a list of commands.");

	w := new(World);
	w.pkgs = new(vector.StringVector);
	w.defs = new(vector.StringVector);
	w.code = new(vector.Vector);

	r := bufio.NewReader(os.Stdin);
	unstable := false;
	for {
		if unstable {
			fmt.Print("! ")
		}

		fmt.Print(strings.Join(w.pkgs.Data(), " ") + "> ");

		line, err := r.ReadString('\n');
		if err != nil {
			break
		}

		w.exec = "";

		switch line[0] {
		case '?':
			fmt.Println("Commands:");
			fmt.Println("\t?: help");
			fmt.Println("\t+ (pkgname): import package");
			fmt.Println("\t-[dpc]: pop last (declaration|package|code)");
			fmt.Println("\t~: reset");
			fmt.Println("\t: (expr): add persistent code");
			fmt.Println("\t!: inspect source");
			continue;
		case '+':
			w.pkgs.Push(line[2 : len(line)-1]);
			unstable = true;
			continue;
		case '-':
			switch line[1] {
			case 'd':
				if w.defs.Len() > 0 {
					w.defs.Pop()
				}
			case 'p':
				if w.pkgs.Len() > 0 {
					if len(line) > 3 {
						for i, v := range w.pkgs.Data() {
							if v == line[3:len(line) - 1] {
								w.pkgs.Delete(i);
								break;
							}
						}
					} else {
						w.pkgs.Pop()
					}
				}
			case 'c':
				fallthrough
			default:
				if w.code.Len() > 0 {
					w.code.Pop()
				}
			}

			if err := compile(w); err.Len() == 0 {
				unstable = false
			}

			continue
		case '~':
			w.pkgs.Resize(0, 0);
			w.defs.Resize(0, 0);
			w.code.Resize(0, 0);
			unstable = false;
		case '\n':
			continue
		case ':':
			tree, err := parser.ParseStmtList("go-repl", line[2:len(line)-1]);
			if err != nil {
				fmt.Println("Parse error:", err);
				continue;
			}

			w.code.Push(tree[0]);
		case '!':
			fmt.Println(w.source())
		default:
			var tree interface{}
			tree, err := parser.ParseStmtList("go-repl", line[0:len(line)-1]);
			if err != nil {
				tree, err = parser.ParseDeclList("go-repl", line[0:len(line)-1]);
				if err != nil {
					fmt.Println("Parse error:", err);
					continue;
				}
			}

			switch tree.(type) {
			case []ast.Stmt:
				for _, v := range tree.([]ast.Stmt) {
					str := new(bytes.Buffer);
					printer.Fprint(str, v);

					switch v.(type) {
					case *ast.AssignStmt:
						w.code.Push(v)
					default:
						w.exec = str.String()
					}
				}
			case []ast.Decl:
				for _, v := range tree.([]ast.Decl) {
					str := new(bytes.Buffer);
					printer.Fprint(str, v);

					w.defs.Push(str.String());
				}
			}
		}

		if err := compile(w); err.Len() > 0 {
			unstable = true;
			fmt.Println("Compile error:", err);
			continue;
		} else if line[0] == ':' {
			unstable = false
		}

		if out, err := run(); err.Len() == 0 {
			fmt.Print(out)
		} else {
			fmt.Println("Runtime error:\n", err)
		}

	}
}
