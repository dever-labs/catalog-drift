package diff

import (
"fmt"
"strings"
)

// diffAsyncAPI compares two AsyncAPI specs at the channel and message-schema level.
func diffAsyncAPI(contractDef string, localContent []byte) ([]Violation, error) {
contract, err := parseSpec(contractDef)
if err != nil {
return nil, fmt.Errorf("parse contract: %w", err)
}
local, err := parseSpec(string(localContent))
if err != nil {
return nil, fmt.Errorf("parse local spec: %w", err)
}

contractChannels := extractChannels(contract)
localChannels := extractChannels(local)

var violations []Violation

// Channels declared in contract but missing from local.
for ch := range contractChannels {
if _, ok := localChannels[ch]; !ok {
violations = append(violations, Violation{
Rule:     RuleMissingChannel,
Path:     "channels." + ch,
Message:  fmt.Sprintf("channel %q is declared in the contract but missing from the local spec", ch),
Severity: SeverityError,
})
}
}

// Channels in local but not declared in contract.
for ch := range localChannels {
if _, ok := contractChannels[ch]; !ok {
violations = append(violations, Violation{
Rule:     RuleUndeclaredEndpoint,
Path:     "channels." + ch,
Message:  fmt.Sprintf("channel %q exists in the local spec but is not declared in the contract", ch),
Severity: SeverityWarning,
})
}
}

// Schema comparison for channels present in both.
for ch := range contractChannels {
localCh, ok := localChannels[ch]
if !ok {
continue
}
contractCh, _ := contractChannels[ch].(map[string]any)
localChMap, _ := localCh.(map[string]any)

for _, dir := range []string{"publish", "subscribe"} {
vs := diffAsyncAPIMessageSchema(ch, dir, contractCh, localChMap)
violations = append(violations, vs...)
}
}

return violations, nil
}

// diffAsyncAPIMessageSchema compares the payload schema for a single
// channel direction (publish or subscribe).
func diffAsyncAPIMessageSchema(channel, direction string, contractCh, localCh map[string]any) []Violation {
contractSchema := asyncAPIPayloadSchema(contractCh, direction)
localSchema := asyncAPIPayloadSchema(localCh, direction)

if contractSchema == nil {
return nil
}

var vs []Violation
contractProps := propertiesFromAsyncSchema(contractSchema)
localProps := propertiesFromAsyncSchema(localSchema)

for field, contractType := range contractProps {
localType, exists := localProps[field]
if !exists {
vs = append(vs, Violation{
Rule:     RuleMissingField,
Path:     fmt.Sprintf("channels.%s.%s.message.payload.%s", channel, direction, field),
Message:  fmt.Sprintf("channel %q %s message: field %q is declared in the contract but missing from the local spec", channel, direction, field),
Severity: SeverityError,
})
} else if contractType != localType && contractType != "" && localType != "" {
vs = append(vs, Violation{
Rule:     RuleTypeMismatch,
Path:     fmt.Sprintf("channels.%s.%s.message.payload.%s", channel, direction, field),
Message:  fmt.Sprintf("channel %q %s message: field %q type changed from %q to %q", channel, direction, field, contractType, localType),
Severity: SeverityWarning,
})
}
}

// Fields in local but not in contract.
for field := range localProps {
if _, ok := contractProps[field]; !ok {
vs = append(vs, Violation{
Rule:     RuleUndeclaredEndpoint,
Path:     fmt.Sprintf("channels.%s.%s.message.payload.%s", channel, direction, field),
Message:  fmt.Sprintf("channel %q %s message: field %q exists in the local spec but is not declared in the contract", channel, direction, field),
Severity: SeverityWarning,
})
}
}

return vs
}

// asyncAPIPayloadSchema extracts the payload schema map from a channel
// object for the given direction (publish or subscribe).
func asyncAPIPayloadSchema(ch map[string]any, direction string) map[string]any {
if ch == nil {
return nil
}
op, _ := ch[direction].(map[string]any)
if op == nil {
return nil
}
msg, _ := op["message"].(map[string]any)
if msg == nil {
return nil
}
payload, _ := msg["payload"].(map[string]any)
return payload
}

// propertiesFromAsyncSchema extracts field → type pairs from an AsyncAPI
// payload schema's properties map.
func propertiesFromAsyncSchema(schema map[string]any) map[string]string {
if schema == nil {
return nil
}
if schema["$ref"] != nil {
return nil
}
props, _ := schema["properties"].(map[string]any)
if props == nil {
return nil
}
result := make(map[string]string, len(props))
for name, v := range props {
pm, _ := v.(map[string]any)
if pm == nil {
result[name] = ""
continue
}
t, _ := pm["type"].(string)
result[name] = strings.TrimSpace(t)
}
return result
}

// extractChannels returns the set of channel names from an AsyncAPI spec.
func extractChannels(spec map[string]any) map[string]any {
channels := nestedMap(spec, "channels")
if channels == nil {
return map[string]any{}
}
return channels
}
