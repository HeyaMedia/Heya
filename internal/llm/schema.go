package llm

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemaValidator enforces the JSON Schema contract of CompleteJSON on the
// client side. Server-side enforcement varies wildly: llama.cpp compiles the
// schema to a grammar, big providers honor response_format, and lax ones
// accept the parameter and then ignore it — so every reply is validated here
// regardless of which path produced it.
//
// Validation is spec-compliant (santhosh-tekuri/jsonschema), notably
// case-SENSITIVE on property names: with additionalProperties:false a reply
// carrying "NAME" instead of "name" is a violation, even though Go's lenient
// json.Unmarshal would have happily filled the struct — consumers persist the
// raw JSON, so a case-mismatched key must never survive.
type schemaValidator struct {
	schema *jsonschema.Schema
}

func newSchemaValidator(raw []byte) (*schemaValidator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("llm: invalid JSON schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", doc); err != nil {
		return nil, fmt.Errorf("llm: invalid JSON schema: %w", err)
	}
	sch, err := compiler.Compile("schema.json")
	if err != nil {
		return nil, fmt.Errorf("llm: invalid JSON schema: %w", err)
	}
	return &schemaValidator{schema: sch}, nil
}

// validate reports nil when raw parses as JSON and satisfies the schema.
// The returned error message lists the concrete violations (capped) so it can
// be fed back to the model as a corrective prompt.
func (v *schemaValidator) validate(raw []byte) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	err = v.schema.Validate(value)
	if err == nil {
		return nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return err
	}
	// Flatten the cause tree into prompt-friendly one-liners. The top-level
	// Error() is multi-line ("- at '/x': …" per leaf) — reuse its formatting
	// but compact and cap it.
	lines := strings.Split(ve.Error(), "\n")
	const maxViolations = 8
	msgs := make([]string, 0, maxViolations+1)
	for _, l := range lines[1:] { // line 0 is the "validation failed" header
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "- "))
		if l == "" {
			continue
		}
		if len(msgs) == maxViolations {
			msgs = append(msgs, fmt.Sprintf("… and %d more violations", len(lines)-1-maxViolations))
			break
		}
		msgs = append(msgs, l)
	}
	if len(msgs) == 0 {
		msgs = append(msgs, strings.TrimSpace(lines[0]))
	}
	return errors.New(strings.Join(msgs, "; "))
}
