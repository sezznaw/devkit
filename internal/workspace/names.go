package workspace

import (
	"fmt"
	"go/token"
	"strings"
)

// Predeclared Go identifiers. A service name becomes a Go package name
// (kitex_gen/<name>), and a package named after one of these shadows the
// builtin inside the generated code and fails to compile.
var predeclared = map[string]bool{
	"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true, "complex128": true,
	"error": true, "float32": true, "float64": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "rune": true, "string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true,
	"uint64": true, "uintptr": true, "true": true, "false": true, "iota": true, "nil": true,
	"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true, "delete": true,
	"imag": true, "len": true, "make": true, "max": true, "min": true, "new": true, "panic": true,
	"print": true, "println": true, "real": true, "recover": true,
}

// Names that break for other reasons (each verified by generating a service).
var reserved = map[string]string{
	"main":     "a package named main cannot be imported",
	"init":     "init is a reserved function name in Go",
	"internal": "Go restricts imports of directories named internal",
	"vendor":   "Go treats a directory named vendor specially",
	"handler":  "it collides with identifiers inside Kitex's generated code",
	"idl":      "idl/ is the project's shared IDL directory",
	"common":   "common/ is the checkout of the shared library",
}

// ValidateServiceName rejects names that would produce a service that cannot
// compile or that collide with the project directory layout.
func ValidateServiceName(name string) error {
	pkg := strings.ReplaceAll(name, "-", "_")
	switch {
	case token.IsKeyword(pkg):
		return fmt.Errorf("%q cannot be used as a service name: it is a Go keyword; try %q", name, name+"-svc")
	case predeclared[pkg]:
		return fmt.Errorf("%q cannot be used as a service name: it is a built-in Go identifier; try %q", name, name+"-svc")
	case reserved[pkg] != "":
		return fmt.Errorf("%q cannot be used as a service name: %s; try %q", name, reserved[pkg], name+"-svc")
	}
	return nil
}
