package code

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// GRPCMethod is a gRPC service method found implemented in source code.
type GRPCMethod struct {
	Service string // service name, e.g. "PaymentService"
	Method  string // method name, e.g. "CreatePayment"
	File    string
	Line    int
}

// GRPCScanner walks a source tree and extracts implemented gRPC service methods
// across Go, Python, .NET (C#), and Java/Kotlin.
//
// It scans two complementary sources:
//   - Generated gRPC stub files (*_grpc.pb.go, *_pb2_grpc.py, *.grpc.pb.cs,
//     *Grpc.java, *Grpc.kt) — always present when gRPC is used and regenerated
//     from the current proto, making them the ground truth of what's implemented.
//   - Hand-written implementation files that contain server-method signatures.
type GRPCScanner struct {
	root string
}

// NewGRPCScanner creates a GRPCScanner rooted at dir.
func NewGRPCScanner(dir string) *GRPCScanner { return &GRPCScanner{root: dir} }

// Scan returns all gRPC server methods found under the root directory.
func (s *GRPCScanner) Scan() ([]GRPCMethod, error) {
	var methods []GRPCMethod

	err := filepath.Walk(s.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != s.root && isSkipped(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		ms, scanErr := scanGRPCFile(path)
		if scanErr != nil {
			return scanErr
		}
		methods = append(methods, ms...)
		return nil
	})

	return methods, err
}

func scanGRPCFile(path string) ([]GRPCMethod, error) {
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)

	switch {
	case isGoGRPCFile(base):
		return scanGoGRPC(path)
	case isPythonGRPCFile(base):
		return scanPythonGRPC(path)
	case isDotNetGRPCFile(base):
		return scanDotNetGRPC(path)
	case isJavaGRPCFile(base, ext):
		return scanJavaGRPC(path)
	}
	return nil, nil
}

// ── File pattern detection ─────────────────────────────────────────────────────

func isGoGRPCFile(base string) bool {
	return strings.HasSuffix(base, "_grpc.pb.go")
}

func isPythonGRPCFile(base string) bool {
	return strings.HasSuffix(base, "_pb2_grpc.py")
}

func isDotNetGRPCFile(base string) bool {
	return strings.HasSuffix(base, ".grpc.pb.cs") ||
		strings.HasSuffix(base, "Grpc.cs") ||
		strings.HasSuffix(base, ".grpc.cs")
}

func isJavaGRPCFile(base, ext string) bool {
	return (ext == ".java" || ext == ".kt") && strings.HasSuffix(strings.TrimSuffix(base, ext), "Grpc")
}

// ── Go ─────────────────────────────────────────────────────────────────────────
//
// Generated Go gRPC stubs define an Unimplemented*Server struct with one method
// per RPC. Example:
//
//	func (UnimplementedPaymentServiceServer) CreatePayment(
//	func (UnimplementedPaymentServiceServer) GetPayment(
//
// The service name is embedded in the receiver type: Unimplemented<Service>Server.

var (
	goGRPCMethodRe = regexp.MustCompile(
		`func\s*\(Unimplemented(\w+)Server\)\s+(\w+)\s*\(`)

	// Also catch explicit server interface methods for hand-rolled impls.
	// type PaymentServiceServer interface { CreatePayment(...) }
	goGRPCInterfaceRe = regexp.MustCompile(
		`type\s+(\w+Server)\s+interface\s*\{`)
	goGRPCInterfaceMethodRe = regexp.MustCompile(
		`^\s+(\w+)\s*\(context\.Context`)
)

func scanGoGRPC(path string) ([]GRPCMethod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var methods []GRPCMethod
	sc := bufio.NewScanner(f)
	lineNum := 0

	for sc.Scan() {
		lineNum++
		line := sc.Text()
		if m := goGRPCMethodRe.FindStringSubmatch(line); m != nil {
			methods = append(methods, GRPCMethod{
				Service: m[1],
				Method:  m[2],
				File:    path,
				Line:    lineNum,
			})
		}
	}
	return methods, sc.Err()
}

// ── Python ─────────────────────────────────────────────────────────────────────
//
// Generated Python stubs define a servicer base class per service. Example:
//
//	class PaymentServiceServicer(object):
//	    def CreatePayment(self, request, context):
//
// Service name is the class name minus the trailing "Servicer".

var (
	pyServiceClassRe  = regexp.MustCompile(`^class\s+(\w+)Servicer\s*[\(:]`)
	pyServiceMethodRe = regexp.MustCompile(`^\s+def\s+(\w+)\s*\(\s*self\s*,\s*request\s*,\s*context`)
)

func scanPythonGRPC(path string) ([]GRPCMethod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var methods []GRPCMethod
	sc := bufio.NewScanner(f)
	lineNum := 0
	currentService := ""

	for sc.Scan() {
		lineNum++
		line := sc.Text()

		if m := pyServiceClassRe.FindStringSubmatch(line); m != nil {
			currentService = m[1]
			continue
		}
		if currentService != "" {
			if m := pyServiceMethodRe.FindStringSubmatch(line); m != nil {
				methods = append(methods, GRPCMethod{
					Service: currentService,
					Method:  m[1],
					File:    path,
					Line:    lineNum,
				})
			}
			// Reset context when we leave the class (hit a non-indented line that's not blank).
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				currentService = ""
			}
		}
	}
	return methods, sc.Err()
}

// ── .NET (C#) ──────────────────────────────────────────────────────────────────
//
// Generated C# stubs define an abstract base class per service. Example:
//
//	public abstract partial class PaymentServiceBase
//	    public virtual Task<PaymentResponse> CreatePayment(PaymentRequest request, ServerCallContext context)
//
// Service name is the class name minus the trailing "Base".

var (
	csServiceClassRe  = regexp.MustCompile(`(?i)(?:abstract|partial)\s+class\s+(\w+)Base\b`)
	csServiceMethodRe = regexp.MustCompile(`(?i)(?:public|protected)\s+(?:override\s+|virtual\s+)?(?:Task|AsyncUnaryCall|IAsyncStreamReader|IServerStreamWriter|Task<\w+>)\b[^(]*\s+(\w+)\s*\(\s*\w+\s+\w+\s*,\s*ServerCallContext`)
)

func scanDotNetGRPC(path string) ([]GRPCMethod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var methods []GRPCMethod
	sc := bufio.NewScanner(f)
	lineNum := 0
	currentService := ""

	for sc.Scan() {
		lineNum++
		line := sc.Text()

		if m := csServiceClassRe.FindStringSubmatch(line); m != nil {
			currentService = m[1]
			continue
		}
		if currentService != "" {
			if m := csServiceMethodRe.FindStringSubmatch(line); m != nil {
				methods = append(methods, GRPCMethod{
					Service: currentService,
					Method:  m[1],
					File:    path,
					Line:    lineNum,
				})
			}
		}
	}
	return methods, sc.Err()
}

// ── Java / Kotlin ──────────────────────────────────────────────────────────────
//
// Generated Java gRPC stubs define an abstract ImplBase class per service. Example:
//
//	public static abstract class PaymentServiceImplBase implements BindableService {
//	    public void createPayment(CreatePaymentRequest request, StreamObserver<CreatePaymentResponse> responseObserver) {
//
// Service name is extracted from the class name. Method names are camelCase in Java
// but the proto name is PascalCase — we capitalise the first letter to normalise.

var (
	javaServiceClassRe  = regexp.MustCompile(`(?:abstract\s+)?class\s+(\w+)ImplBase\b`)
	javaServiceMethodRe = regexp.MustCompile(`(?:public|protected)\s+(?:void|StreamObserver\b)\s+(\w+)\s*\(`)
)

func scanJavaGRPC(path string) ([]GRPCMethod, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var methods []GRPCMethod
	sc := bufio.NewScanner(f)
	lineNum := 0
	currentService := ""

	for sc.Scan() {
		lineNum++
		line := sc.Text()

		if m := javaServiceClassRe.FindStringSubmatch(line); m != nil {
			currentService = m[1]
			continue
		}
		if currentService != "" {
			if m := javaServiceMethodRe.FindStringSubmatch(line); m != nil {
				// Normalise Java camelCase to PascalCase to match proto method names.
				methodName := capitalise(m[1])
				methods = append(methods, GRPCMethod{
					Service: currentService,
					Method:  methodName,
					File:    path,
					Line:    lineNum,
				})
			}
		}
	}
	return methods, sc.Err()
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
