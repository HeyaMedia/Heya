package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

// schemaValidator enforces the JSON Schema contract of CompleteJSON on the
// client side. Server-side enforcement varies wildly: llama.cpp compiles the
// schema to a grammar, big providers honor response_format, and lax ones
// accept the parameter and then ignore it — so every reply is validated here
// regardless of which path produced it. Reuses huma's validator (already a
// dependency for the API layer), which covers the subset our consumers
// author: type, required, enum, properties, items, additionalProperties,
// bounds, patterns. Schemas must be self-contained ($ref is not resolved).
type schemaValidator struct {
	schema   *huma.Schema
	registry huma.Registry
}

func newSchemaValidator(raw []byte) (*schemaValidator, error) {
	var s huma.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("llm: invalid JSON schema: %w", err)
	}
	s.PrecomputeMessages()
	return &schemaValidator{
		schema:   &s,
		registry: huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer),
	}, nil
}

// validate reports nil when raw parses as JSON and satisfies the schema.
// The returned error message lists the concrete violations (capped) so it can
// be fed back to the model as a corrective prompt.
func (v *schemaValidator) validate(raw []byte) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("not valid JSON: %w", err)
	}
	res := &huma.ValidateResult{}
	huma.Validate(v.registry, v.schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, value, res)
	if len(res.Errors) == 0 {
		return nil
	}
	const maxViolations = 8
	msgs := make([]string, 0, min(len(res.Errors), maxViolations))
	for i, e := range res.Errors {
		if i == maxViolations {
			msgs = append(msgs, fmt.Sprintf("… and %d more violations", len(res.Errors)-maxViolations))
			break
		}
		msgs = append(msgs, e.Error())
	}
	return errors.New(strings.Join(msgs, "; "))
}
