// The schema-generate binary reads the JSON schema files passed as arguments
// and outputs the corresponding Go structs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/a-h/generate"
	"github.com/a-h/generate/jsonschema"
)

var (
	o = flag.String("o", "", "The output file for the schema.")
	p = flag.String("p", "main", "The package that the structs are created in.")
	i = flag.String("i", "", "A single file path (used for backwards compatibility).")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "  paths")
		fmt.Fprintln(os.Stderr, "\tThe input JSON Schema files.")
	}

	flag.Parse()

	inputFiles := flag.Args()
	if *i != "" {
		inputFiles = append(inputFiles, *i)
	}
	if len(inputFiles) == 0 {
		fmt.Fprintln(os.Stderr, "No input JSON Schema files.")
		flag.Usage()
		os.Exit(1)
	}

	schemas := make([]*jsonschema.Schema, len(inputFiles))
	for i, file := range inputFiles {
		b, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to read the input file with error ", err)
			return
		}

		schemas[i], err = jsonschema.Parse(string(b))
		if err != nil {
			if jsonError, ok := err.(*json.SyntaxError); ok {
				line, character, lcErr := lineAndCharacter(b, int(jsonError.Offset))
				fmt.Fprintf(os.Stderr, "Cannot parse JSON schema due to a syntax error at %s line %d, character %d: %v\n", file, line, character, jsonError.Error())
				if lcErr != nil {
					fmt.Fprintf(os.Stderr, "Couldn't find the line and character position of the error due to error %v\n", lcErr)
				}
				return
			}
			if jsonError, ok := err.(*json.UnmarshalTypeError); ok {
				line, character, lcErr := lineAndCharacter(b, int(jsonError.Offset))
				fmt.Fprintf(os.Stderr, "The JSON type '%v' cannot be converted into the Go '%v' type on struct '%s', field '%v'. See input file %s line %d, character %d\n", jsonError.Value, jsonError.Type.Name(), jsonError.Struct, jsonError.Field, file, line, character)
				if lcErr != nil {
					fmt.Fprintf(os.Stderr, "Couldn't find the line and character position of the error due to error %v\n", lcErr)
				}
				return
			}
			fmt.Fprintf(os.Stderr, "Failed to parse the input JSON schema file %s with error %v\n", file, err)
			return
		}
	}

	g := generate.New(schemas...)

	structs, aliases, err := g.CreateTypes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failure generating structs: ", err)
	}

	var w io.Writer = os.Stdout

	if *o != "" {
		w, err = os.Create(*o)

		if err != nil {
			fmt.Fprintln(os.Stderr, "Error opening output file: ", err)
			return
		}
	}

	output(w, structs, aliases)
}

func lineAndCharacter(bytes []byte, offset int) (line int, character int, err error) {
	lf := byte(0x0A)

	if offset > len(bytes) {
		return 0, 0, fmt.Errorf("couldn't find offset %d in %d bytes", offset, len(bytes))
	}

	// Humans tend to count from 1.
	line = 1

	for i, b := range bytes {
		if b == lf {
			line++
			character = 0
		}
		character++
		if i == offset {
			return line, character, nil
		}
	}

	return 0, 0, fmt.Errorf("couldn't find offset %d in %d bytes", offset, len(bytes))
}

func getOrderedFieldNames(m map[string]generate.Field) []string {
	keys := make([]string, len(m))
	idx := 0
	for k := range m {
		keys[idx] = k
		idx++
	}
	sort.Strings(keys)
	return keys
}

func getOrderedStructNames(m map[string]generate.Struct) []string {
	keys := make([]string, len(m))
	idx := 0
	for k := range m {
		keys[idx] = k
		idx++
	}
	sort.Strings(keys)
	return keys
}

func output(w io.Writer, structs map[string]generate.Struct, aliases map[string]generate.Field) {
	buf := &bytes.Buffer{}

	fmt.Fprintln(buf, "// Code generated by schema-generate. DO NOT EDIT.")
	fmt.Fprintln(buf)
	fmt.Fprintf(buf, "package %v\n", *p)

	for _, k := range getOrderedFieldNames(aliases) {
		a := aliases[k]

		fmt.Fprintln(buf, "")
		fmt.Fprintf(buf, "// %s\n", a.Name)
		fmt.Fprintf(buf, "type %s %s\n", a.Name, a.Type)
	}

	for _, k := range getOrderedStructNames(structs) {
		s := structs[k]

		fmt.Fprintln(buf, "")
		outputNameAndDescriptionComment(s.Name, s.Description, buf)
		fmt.Fprintf(buf, "type %s struct {\n", s.Name)

		for _, fieldKey := range getOrderedFieldNames(s.Fields) {
			f := s.Fields[fieldKey]

			// Only apply omitempty if the field is not required.
			omitempty := ",omitempty"
			if f.Required {
				omitempty = ""
			}

			fmt.Fprintf(buf, "  %s %s `json:\"%s%s\"`\n", f.Name, f.Type, f.JSONName, omitempty)
		}

		fmt.Fprintln(buf, "}")
	}

	data, err := format.Source(buf.Bytes())
	if err != nil {
		log.Panicf("format error: %v\n", err)
	}

	w.Write(data)
}

func outputNameAndDescriptionComment(name, description string, w io.Writer) {
	if strings.Index(description, "\n") == -1 {
		fmt.Fprintf(w, "// %s %s\n", name, description)
		return
	}

	dl := strings.Split(description, "\n")
	fmt.Fprintf(w, "// %s %s\n", name, strings.Join(dl, "\n// "))
}
