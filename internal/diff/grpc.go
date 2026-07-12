package diff

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/jhump/protoreflect/desc/protoparse"
	"google.golang.org/protobuf/types/descriptorpb"
)

// diffGRPC compares two proto definitions at the service, method, and
// message-field level using a proper proto parser. Gracefully falls back to
// the regex approach when the parser cannot handle the input (e.g. protos that
// import external well-known types without providing them).
func diffGRPC(contractDef string, localContent []byte) ([]Violation, error) {
	contractFD, contractErr := parseProto("contract.proto", contractDef)
	localFD, localErr := parseProto("local.proto", string(localContent))

	// If either parse fails, fall back to the lighter regex approach so the
	// tool still produces useful output rather than a parse error.
	if contractErr != nil || localErr != nil {
		return diffGRPCRegex(contractDef, string(localContent))
	}

	return diffProtoDescriptors(contractFD, localFD), nil
}

// parseProto parses a proto source string into a FileDescriptorProto.
// It uses ParseFilesButDoNotLink so that external imports are not required.
func parseProto(filename, content string) (*descriptorpb.FileDescriptorProto, error) {
	p := protoparse.Parser{
		Accessor: func(fn string) (io.ReadCloser, error) {
			if fn == filename {
				return io.NopCloser(strings.NewReader(content)), nil
			}
			// Return empty content for unresolvable imports so parsing can
			// continue without them rather than failing entirely.
			return io.NopCloser(strings.NewReader("")), nil
		},
	}
	fds, err := p.ParseFilesButDoNotLink(filename)
	if err != nil {
		return nil, err
	}
	if len(fds) == 0 {
		return nil, fmt.Errorf("no descriptor returned for %q", filename)
	}
	return fds[0], nil
}

// diffProtoDescriptors compares a contract FileDescriptorProto against a local
// one and returns all violations found.
func diffProtoDescriptors(contract, local *descriptorpb.FileDescriptorProto) []Violation {
	var violations []Violation

	contractSvcs := servicesByName(contract)
	localSvcs    := servicesByName(local)

	// Services in contract but missing locally.
	for name, csvc := range contractSvcs {
		lsvc, ok := localSvcs[name]
		if !ok {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     name,
				Message:  fmt.Sprintf("service %q is declared in the contract but missing from the local proto", name),
				Severity: SeverityError,
			})
			continue
		}
		violations = append(violations, diffServiceMethods(name, csvc, lsvc)...)
	}

	// Services in local but not in contract.
	for name := range localSvcs {
		if _, ok := contractSvcs[name]; !ok {
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredRPCMethod,
				Path:     name,
				Message:  fmt.Sprintf("service %q exists locally but is not declared in the contract", name),
				Severity: SeverityWarning,
			})
		}
	}

	// Compare message types that are referenced by contracted methods.
	contractMsgs := messagesByName(contract)
	localMsgs    := messagesByName(local)

	// Only compare messages that appear in the contract (local-only messages are fine).
	for name, cmsg := range contractMsgs {
		lmsg, ok := localMsgs[name]
		if !ok {
			// Missing message only matters if it's actually used by a service method;
			// flag it as a warning since the method check will already surface the error.
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     name,
				Message:  fmt.Sprintf("message %q is defined in the contract but missing from the local proto", name),
				Severity: SeverityWarning,
			})
			continue
		}
		violations = append(violations, diffMessageFields(name, cmsg, lmsg)...)
	}

	return violations
}

func diffServiceMethods(svcName string, contract, local *descriptorpb.ServiceDescriptorProto) []Violation {
	var violations []Violation

	contractMethods := methodsByName(contract)
	localMethods    := methodsByName(local)

	for name, cm := range contractMethods {
		lm, ok := localMethods[name]
		key := svcName + "." + name
		if !ok {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc %q in service %q is declared in the contract but missing locally", name, svcName),
				Severity: SeverityError,
			})
			continue
		}
		// Check request/response type names (stripping leading dot).
		cIn  := strings.TrimPrefix(cm.GetInputType(), ".")
		lIn  := strings.TrimPrefix(lm.GetInputType(), ".")
		cOut := strings.TrimPrefix(cm.GetOutputType(), ".")
		lOut := strings.TrimPrefix(lm.GetOutputType(), ".")

		if cIn != lIn {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc %q request type changed: contract has %q, local has %q", name, cIn, lIn),
				Severity: SeverityError,
			})
		}
		if cOut != lOut {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc %q response type changed: contract has %q, local has %q", name, cOut, lOut),
				Severity: SeverityError,
			})
		}
	}

	for name := range localMethods {
		if _, ok := contractMethods[name]; !ok {
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredRPCMethod,
				Path:     svcName + "." + name,
				Message:  fmt.Sprintf("rpc %q in service %q exists locally but is not declared in the contract", name, svcName),
				Severity: SeverityWarning,
			})
		}
	}

	return violations
}

func diffMessageFields(msgName string, contract, local *descriptorpb.DescriptorProto) []Violation {
	var violations []Violation

	contractFields := fieldsByNumber(contract)
	localFields    := fieldsByNumber(local)
	localByName    := fieldsByName(local)

	for num, cf := range contractFields {
		lf, ok := localFields[num]
		if !ok {
			// Field removed or renumbered — check if a field with the same name exists elsewhere.
			if _, nameExists := localByName[cf.GetName()]; nameExists {
				violations = append(violations, Violation{
					Rule:     RuleMissingRPCMethod,
					Path:     fmt.Sprintf("%s.%s", msgName, cf.GetName()),
					Message:  fmt.Sprintf("field %q in message %q: field number changed from %d (breaking — wire format incompatible)", cf.GetName(), msgName, num),
					Severity: SeverityError,
				})
			} else {
				violations = append(violations, Violation{
					Rule:     RuleMissingRPCMethod,
					Path:     fmt.Sprintf("%s.%s", msgName, cf.GetName()),
					Message:  fmt.Sprintf("field %d (%q) in message %q was removed (breaking — existing clients will break)", num, cf.GetName(), msgName),
					Severity: SeverityError,
				})
			}
			continue
		}

		// Field type changed.
		if cf.GetType() != lf.GetType() {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     fmt.Sprintf("%s.%s", msgName, cf.GetName()),
				Message:  fmt.Sprintf("field %q in message %q: type changed from %s to %s (breaking)", cf.GetName(), msgName, fieldTypeName(cf.GetType()), fieldTypeName(lf.GetType())),
				Severity: SeverityError,
			})
		}

		// Field name changed (same number, different name — technically non-breaking for binary,
		// but breaking for JSON/proto-name-based serialization).
		if cf.GetName() != lf.GetName() {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     fmt.Sprintf("%s[%d]", msgName, num),
				Message:  fmt.Sprintf("field number %d in message %q: name changed from %q to %q (breaking for JSON encoding)", num, msgName, cf.GetName(), lf.GetName()),
				Severity: SeverityWarning,
			})
		}

		// Label changed (e.g. optional → repeated).
		if cf.GetLabel() != lf.GetLabel() {
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     fmt.Sprintf("%s.%s", msgName, cf.GetName()),
				Message:  fmt.Sprintf("field %q in message %q: label changed from %s to %s (breaking)", cf.GetName(), msgName, labelName(cf.GetLabel()), labelName(lf.GetLabel())),
				Severity: SeverityError,
			})
		}
	}

	// Fields added locally (new field numbers) are backward compatible.
	for num, lf := range localFields {
		if _, ok := contractFields[num]; !ok {
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredRPCMethod,
				Path:     fmt.Sprintf("%s.%s", msgName, lf.GetName()),
				Message:  fmt.Sprintf("field %d (%q) added to message %q locally but not in contract — register the updated spec in Backstage", num, lf.GetName(), msgName),
				Severity: SeverityWarning,
			})
		}
	}

	return violations
}

// ── Index helpers ─────────────────────────────────────────────────────────────

func servicesByName(fd *descriptorpb.FileDescriptorProto) map[string]*descriptorpb.ServiceDescriptorProto {
	m := make(map[string]*descriptorpb.ServiceDescriptorProto, len(fd.GetService()))
	for _, s := range fd.GetService() {
		m[s.GetName()] = s
	}
	return m
}

func methodsByName(sd *descriptorpb.ServiceDescriptorProto) map[string]*descriptorpb.MethodDescriptorProto {
	m := make(map[string]*descriptorpb.MethodDescriptorProto, len(sd.GetMethod()))
	for _, method := range sd.GetMethod() {
		m[method.GetName()] = method
	}
	return m
}

func messagesByName(fd *descriptorpb.FileDescriptorProto) map[string]*descriptorpb.DescriptorProto {
	m := make(map[string]*descriptorpb.DescriptorProto, len(fd.GetMessageType()))
	for _, msg := range fd.GetMessageType() {
		m[msg.GetName()] = msg
	}
	return m
}

func fieldsByNumber(msg *descriptorpb.DescriptorProto) map[int32]*descriptorpb.FieldDescriptorProto {
	m := make(map[int32]*descriptorpb.FieldDescriptorProto, len(msg.GetField()))
	for _, f := range msg.GetField() {
		m[f.GetNumber()] = f
	}
	return m
}

func fieldsByName(msg *descriptorpb.DescriptorProto) map[string]*descriptorpb.FieldDescriptorProto {
	m := make(map[string]*descriptorpb.FieldDescriptorProto, len(msg.GetField()))
	for _, f := range msg.GetField() {
		m[f.GetName()] = f
	}
	return m
}

func fieldTypeName(t descriptorpb.FieldDescriptorProto_Type) string {
	return strings.ToLower(strings.TrimPrefix(t.String(), "TYPE_"))
}

func labelName(l descriptorpb.FieldDescriptorProto_Label) string {
	return strings.ToLower(strings.TrimPrefix(l.String(), "LABEL_"))
}

// ── Regex fallback ────────────────────────────────────────────────────────────

var (
	rpcMethodRe = regexp.MustCompile(`\brpc\s+(\w+)\s*\(`)
	serviceRe   = regexp.MustCompile(`\bservice\s+(\w+)\s*\{`)
)

// diffGRPCRegex is the original lightweight regex-based diff, used as a fallback
// when the proto parser cannot handle the input (e.g. missing imports).
func diffGRPCRegex(contractDef, localContent string) ([]Violation, error) {
	contractMethods := extractRPCMethods(contractDef)
	localMethods := extractRPCMethods(localContent)

	var violations []Violation
	for key, service := range contractMethods {
		if _, ok := localMethods[key]; !ok {
			parts := strings.SplitN(key, ".", 2)
			method := parts[len(parts)-1]
			violations = append(violations, Violation{
				Rule:     RuleMissingRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc method %q (service %q) is declared in the contract but missing from the local proto", method, service),
				Severity: SeverityError,
			})
		}
	}
	for key, service := range localMethods {
		if _, ok := contractMethods[key]; !ok {
			parts := strings.SplitN(key, ".", 2)
			method := parts[len(parts)-1]
			violations = append(violations, Violation{
				Rule:     RuleUndeclaredRPCMethod,
				Path:     key,
				Message:  fmt.Sprintf("rpc method %q (service %q) exists in the local proto but is not declared in the contract", method, service),
				Severity: SeverityWarning,
			})
		}
	}
	return violations, nil
}

// extractRPCMethods returns a map of "ServiceName.MethodName" → ServiceName.
func extractRPCMethods(content string) map[string]string {
	result := make(map[string]string)
	serviceMatches := serviceRe.FindAllStringSubmatchIndex(content, -1)

	for i, sm := range serviceMatches {
		serviceName := content[sm[2]:sm[3]]
		blockStart := sm[0]
		blockEnd := len(content)
		if i+1 < len(serviceMatches) {
			blockEnd = serviceMatches[i+1][0]
		}
		block := content[blockStart:blockEnd]
		for _, mm := range rpcMethodRe.FindAllStringSubmatch(block, -1) {
			key := serviceName + "." + mm[1]
			result[key] = serviceName
		}
	}
	if len(serviceMatches) == 0 {
		for _, mm := range rpcMethodRe.FindAllStringSubmatch(content, -1) {
			result["unknown."+mm[1]] = "unknown"
		}
	}
	return result
}
