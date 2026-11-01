package executor

import (
	"encoding/json"
	"fmt"
)

// validateInput checks a small subset of JSON Schema: object type,
// required properties, and property types string|integer|number|boolean.
func validateInput(schema json.RawMessage, input map[string]any) error {
	var doc struct {
		Type       string                    `json:"type"`
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return err
	}
	if doc.Type != "object" {
		return fmt.Errorf("schema type")
	}
	if input == nil {
		input = map[string]any{}
	}
	for _, req := range doc.Required {
		if _, ok := input[req]; !ok {
			return fmt.Errorf("missing %s", req)
		}
	}
	for k, v := range input {
		spec, ok := doc.Properties[k]
		if !ok {
			return fmt.Errorf("unexpected %s", k)
		}
		want, _ := spec["type"].(string)
		if !typeOK(want, v) {
			return fmt.Errorf("%s: want %s", k, want)
		}
	}
	return nil
}

func typeOK(want string, v any) bool {
	switch want {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "integer":
		switch n := v.(type) {
		case float64:
			return n == float64(int64(n))
		case int:
			return true
		case json.Number:
			_, err := n.Int64()
			return err == nil
		default:
			return false
		}
	case "number":
		_, ok := v.(float64)
		return ok || typeOK("integer", v)
	case "":
		return true
	default:
		return false
	}
}
