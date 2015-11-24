package dartlang

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"gopkg.in/yaml.v2"

	"github.com/Workiva/frugal/compiler/generator"
	"github.com/Workiva/frugal/compiler/globals"
	"github.com/Workiva/frugal/compiler/parser"
)

const (
	lang               = "dart"
	defaultOutputDir   = "gen-dart"
	minimumDartVersion = "1.12.0"
	tab                = "  "
	tabtab             = tab + tab
	tabtabtab          = tab + tab + tab
)

type Generator struct {
	*generator.BaseGenerator
}

func NewGenerator(options map[string]string) generator.MultipleFileGenerator {
	return &Generator{&generator.BaseGenerator{Options: options}}
}

func (g *Generator) GenerateThrift() bool {
	return false
}

func (g *Generator) GetOutputDir(dir string, f *parser.Frugal) string {
	if pkg, ok := f.Thrift.Namespaces[lang]; ok {
		dir = filepath.Join(dir, toLibraryName(pkg))
	} else {
		dir = filepath.Join(dir, f.Name)
	}
	return dir
}

func (g *Generator) DefaultOutputDir() string {
	return defaultOutputDir
}

func (g *Generator) GenerateDependencies(f *parser.Frugal, dir string) error {
	if err := g.addToPubspec(f, dir); err != nil {
		return err
	}
	if err := g.exportClasses(f, dir); err != nil {
		return err
	}
	return nil
}

type pubspec struct {
	Name         string                      `yaml:"name"`
	Version      string                      `yaml:"version"`
	Description  string                      `yaml:"description"`
	Environment  env                         `yaml:"environment"`
	Dependencies map[interface{}]interface{} `yaml:"dependencies"`
}

type env struct {
	SDK string `yaml:"sdk"`
}

type dep struct {
	Git  gitDep `yaml:"git,omitempty"`
	Path string `yaml:"path,omitempty"`
}

type gitDep struct {
	URL string `yaml:"url"`
}

func (g *Generator) addToPubspec(f *parser.Frugal, dir string) error {
	pubFilePath := filepath.Join(dir, "pubspec.yaml")

	deps := map[interface{}]interface{}{
		"thrift": dep{Git: gitDep{URL: "git@github.com:Workiva/thrift-dart.git"}},
		"frugal": dep{Git: gitDep{URL: "git@github.com:Workiva/frugal-dart.git"}},
	}

	for _, include := range f.ReferencedIncludes() {
		namespace, ok := f.NamespaceForInclude(include, lang)
		if !ok {
			namespace = include
		}
		deps[toLibraryName(namespace)] = dep{Path: "../" + toLibraryName(namespace)}
	}

	namespace, ok := f.Thrift.Namespaces[lang]
	if !ok {
		namespace = f.Name
	}

	ps := &pubspec{
		Name:        strings.ToLower(toLibraryName(namespace)),
		Version:     globals.Version,
		Description: "Autogenerated by the frugal compiler",
		Environment: env{
			SDK: "^" + minimumDartVersion,
		},
		Dependencies: deps,
	}

	d, err := yaml.Marshal(&ps)
	if err != nil {
		return err
	}
	// create and write to new file
	newPubFile, err := os.Create(pubFilePath)
	defer newPubFile.Close()
	if err != nil {
		return err
	}
	if _, err := newPubFile.Write(d); err != nil {
		return err
	}
	return nil
}

func (g *Generator) exportClasses(f *parser.Frugal, dir string) error {
	filename := strings.ToLower(f.Name)
	if ns, ok := f.Thrift.Namespaces[lang]; ok {
		filename = strings.ToLower(toLibraryName(ns))
	}
	dartFile := fmt.Sprintf("%s.%s", filename, lang)
	mainFilePath := filepath.Join(dir, "lib", dartFile)
	mainFile, err := os.OpenFile(mainFilePath, syscall.O_RDWR, 0777)
	defer mainFile.Close()
	if err != nil {
		return err
	}

	exports := "\n"
	for _, scope := range f.Scopes {
		exports += fmt.Sprintf("export 'src/%s%s.%s' show %sPublisher, %sSubscriber;\n",
			generator.FilePrefix, strings.ToLower(scope.Name), lang, scope.Name, scope.Name)
	}
	stat, err := mainFile.Stat()
	if err != nil {
		return err
	}
	_, err = mainFile.WriteAt([]byte(exports), stat.Size())
	return err
}

func (g *Generator) GenerateFile(name, outputDir string, fileType generator.FileType) (*os.File, error) {
	if fileType != generator.CombinedScopeFile {
		return nil, fmt.Errorf("frugal: Bad file type for dartlang generator: %s", fileType)
	}
	outputDir = filepath.Join(outputDir, "lib")
	outputDir = filepath.Join(outputDir, "src")
	return g.CreateFile(strings.ToLower(name), outputDir, lang, true)
}

func (g *Generator) GenerateDocStringComment(file *os.File) error {
	comment := fmt.Sprintf(
		"// Autogenerated by Frugal Compiler (%s)\n"+
			"// DO NOT EDIT UNLESS YOU ARE SURE THAT YOU KNOW WHAT YOU ARE DOING",
		globals.Version)

	_, err := file.WriteString(comment)
	return err
}

func (g *Generator) GenerateServicePackage(file *os.File, f *parser.Frugal, s *parser.Service) error {
	return nil
}

func (g *Generator) GenerateScopePackage(file *os.File, f *parser.Frugal, s *parser.Scope) error {
	pkg, ok := f.Thrift.Namespaces[lang]
	if ok {
		components := generator.GetPackageComponents(pkg)
		pkg = components[len(components)-1]
	} else {
		pkg = f.Name
	}
	_, err := file.WriteString(fmt.Sprintf("library %s.src.%s%s;", pkg,
		generator.FilePrefix, strings.ToLower(s.Name)))
	return err
}

func (g *Generator) GenerateServiceImports(file *os.File, s *parser.Service) error {
	return nil
}

func (g *Generator) GenerateScopeImports(file *os.File, f *parser.Frugal, s *parser.Scope) error {
	imports := "import 'dart:async';\n\n"
	imports += "import 'package:thrift/thrift.dart' as thrift;\n"
	imports += "import 'package:frugal/frugal.dart' as frugal;\n\n"
	for _, include := range s.ReferencedIncludes() {
		namespace, ok := s.Frugal.NamespaceForInclude(include, lang)
		if !ok {
			namespace = include
		}
		namespace = strings.ToLower(toLibraryName(namespace))
		imports += fmt.Sprintf("import 'package:%s/%s.dart' as t_%s;\n", namespace, namespace, namespace)
	}

	// Import same-package references.
	params := make(map[string]bool)
	for _, op := range s.Operations {
		if !strings.Contains(op.Param, ".") {
			params[op.Param] = true
		}
	}
	for param, _ := range params {
		lowerParam := strings.ToLower(param)
		imports += fmt.Sprintf("import '%s.dart' as t_%s;\n", lowerParam, lowerParam)
	}

	_, err := file.WriteString(imports)
	return err
}

func (g *Generator) GenerateConstants(file *os.File, name string) error {
	constants := fmt.Sprintf("const String delimiter = '%s';", globals.TopicDelimiter)
	_, err := file.WriteString(constants)
	return err
}

func (g *Generator) GeneratePublisher(file *os.File, scope *parser.Scope) error {
	publishers := ""
	if scope.Comment != nil {
		publishers += g.GenerateInlineComment(scope.Comment, "/")
	}
	publishers += fmt.Sprintf("class %sPublisher {\n", strings.Title(scope.Name))
	publishers += tab + "frugal.Transport transport;\n"
	publishers += tab + "thrift.TProtocol protocol;\n"
	publishers += tab + "int seqId;\n\n"

	publishers += fmt.Sprintf(tab+"%sPublisher(frugal.Provider provider) {\n", strings.Title(scope.Name))
	publishers += tabtab + "var tp = provider.newTransportProtocol();\n"
	publishers += tabtab + "transport = tp.transport;\n"
	publishers += tabtab + "protocol = tp.protocol;\n"
	publishers += tabtab + "seqId = 0;\n"
	publishers += tab + "}\n\n"

	args := ""
	if len(scope.Prefix.Variables) > 0 {
		for _, variable := range scope.Prefix.Variables {
			args = fmt.Sprintf("%sString %s, ", args, variable)
		}
	}
	prefix := ""
	for _, op := range scope.Operations {
		publishers += prefix
		prefix = "\n\n"
		if op.Comment != nil {
			publishers += g.GenerateInlineComment(op.Comment, tab+"/")
		}
		publishers += fmt.Sprintf(tab+"Future publish%s(%s%s req) {\n", op.Name, args, g.qualifiedParamName(op))
		publishers += fmt.Sprintf(tabtab+"var op = \"%s\";\n", op.Name)
		publishers += fmt.Sprintf(tabtab+"var prefix = \"%s\";\n", generatePrefixStringTemplate(scope))
		publishers += tabtab + "var topic = \"${prefix}" + strings.Title(scope.Name) + "${delimiter}${op}\";\n"
		publishers += tabtab + "transport.preparePublish(topic);\n"
		publishers += tabtab + "var oprot = protocol;\n"
		publishers += tabtab + "seqId++;\n"
		publishers += tabtab + "var msg = new thrift.TMessage(op, thrift.TMessageType.CALL, seqId);\n"
		publishers += tabtab + "oprot.writeMessageBegin(msg);\n"
		publishers += tabtab + "req.write(oprot);\n"
		publishers += tabtab + "oprot.writeMessageEnd();\n"
		publishers += tabtab + "return oprot.transport.flush();\n"
		publishers += tab + "}\n"
	}

	publishers += "}\n"

	_, err := file.WriteString(publishers)
	return err
}

func generatePrefixStringTemplate(scope *parser.Scope) string {
	if scope.Prefix.String == "" {
		return ""
	}
	template := ""
	template += scope.Prefix.Template()
	template += globals.TopicDelimiter
	if len(scope.Prefix.Variables) == 0 {
		return template
	}
	vars := make([]interface{}, len(scope.Prefix.Variables))
	for i, variable := range scope.Prefix.Variables {
		vars[i] = fmt.Sprintf("${%s}", variable)
	}
	template = fmt.Sprintf(template, vars...)
	return template
}

func (g *Generator) GenerateSubscriber(file *os.File, scope *parser.Scope) error {
	subscribers := ""
	if scope.Comment != nil {
		subscribers += g.GenerateInlineComment(scope.Comment, "/")
	}
	subscribers += fmt.Sprintf("class %sSubscriber {\n", strings.Title(scope.Name))
	subscribers += tab + "final frugal.Provider provider;\n\n"

	subscribers += fmt.Sprintf(tab+"%sSubscriber(this.provider) {}\n\n", strings.Title(scope.Name))

	args := ""
	if len(scope.Prefix.Variables) > 0 {
		for _, variable := range scope.Prefix.Variables {
			args = fmt.Sprintf("%sString %s, ", args, variable)
		}
	}
	prefix := ""
	for _, op := range scope.Operations {
		subscribers += prefix
		prefix = "\n\n"
		if op.Comment != nil {
			subscribers += g.GenerateInlineComment(op.Comment, tab+"/")
		}
		subscribers += fmt.Sprintf(tab+"Future<frugal.Subscription> subscribe%s(%sdynamic on%s(%s req)) async {\n",
			op.Name, args, op.ParamName(), g.qualifiedParamName(op))
		subscribers += fmt.Sprintf(tabtab+"var op = \"%s\";\n", op.Name)
		subscribers += fmt.Sprintf(tabtab+"var prefix = \"%s\";\n", generatePrefixStringTemplate(scope))
		subscribers += tabtab + "var topic = \"${prefix}" + strings.Title(scope.Name) + "${delimiter}${op}\";\n"
		subscribers += tabtab + "var tp = provider.newTransportProtocol();\n"
		subscribers += tabtab + "await tp.transport.subscribe(topic);\n"
		subscribers += tabtab + "tp.transport.signalRead.listen((_) {\n"
		subscribers += fmt.Sprintf(tabtabtab+"on%s(_recv%s(op, tp.protocol));\n", op.ParamName(), op.Name)
		subscribers += tabtab + "});\n"
		subscribers += tabtab + "var sub = new frugal.Subscription(topic, tp.transport);\n"
		subscribers += tabtab + "tp.transport.error.listen((Error e) {;\n"
		subscribers += tabtabtab + "sub.signal(e);\n"
		subscribers += tabtab + "});\n"
		subscribers += tabtab + "return sub;\n"
		subscribers += tab + "}\n\n"

		subscribers += fmt.Sprintf(tab+"%s _recv%s(String op, thrift.TProtocol iprot) {\n",
			g.qualifiedParamName(op), op.Name)
		subscribers += tabtab + "var tMsg = iprot.readMessageBegin();\n"
		subscribers += tabtab + "if (tMsg.name != op) {\n"
		subscribers += tabtabtab + "thrift.TProtocolUtil.skip(iprot, thrift.TType.STRUCT);\n"
		subscribers += tabtabtab + "iprot.readMessageEnd();\n"
		subscribers += tabtabtab + "throw new thrift.TApplicationError(\n"
		subscribers += tabtabtab + "thrift.TApplicationErrorType.UNKNOWN_METHOD, tMsg.name);\n"
		subscribers += tabtab + "}\n"
		subscribers += fmt.Sprintf(tabtab+"var req = new %s();\n", g.qualifiedParamName(op))
		subscribers += tabtab + "req.read(iprot);\n"
		subscribers += tabtab + "iprot.readMessageEnd();\n"
		subscribers += tabtab + "return req;\n"
		subscribers += tab + "}\n"
	}

	subscribers += "}\n"

	_, err := file.WriteString(subscribers)
	return err
}

func (g *Generator) GenerateService(file *os.File, p *parser.Frugal, s *parser.Service) error {
	return nil
}

func (g *Generator) qualifiedParamName(op *parser.Operation) string {
	param := op.ParamName()
	include := op.IncludeName()
	if include != "" {
		namespace, ok := g.Frugal.NamespaceForInclude(include, lang)
		if !ok {
			namespace = include
		}
		namespace = toLibraryName(namespace)
		param = fmt.Sprintf("t_%s.%s", strings.ToLower(namespace), param)
	} else {
		param = fmt.Sprintf("t_%s.%s", strings.ToLower(param), param)
	}
	return param
}

func toLibraryName(name string) string {
	return strings.Replace(name, ".", "_", -1)
}
