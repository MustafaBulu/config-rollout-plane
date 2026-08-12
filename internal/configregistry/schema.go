package configregistry

import (
	"bytes"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

type SchemaValidator interface {
	ValidateSchema(schema []byte) error
	ValidateValue(schema []byte, value []byte) error
}

type JSONSchemaValidator struct{}

func (JSONSchemaValidator) ValidateSchema(schema []byte) error {
	_, err := compileSchema(schema)
	if err != nil {
		return fmt.Errorf("%w: schema validation failed: %v", ErrInvalidInput, err)
	}
	return nil
}

func (JSONSchemaValidator) ValidateValue(schema []byte, value []byte) error {
	compiled, err := compileSchema(schema)
	if err != nil {
		return fmt.Errorf("%w: schema validation failed: %v", ErrInvalidInput, err)
	}

	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("%w: invalid config value json: %v", ErrInvalidInput, err)
	}

	if err := compiled.Validate(document); err != nil {
		return fmt.Errorf("%w: config value does not match schema: %v", ErrInvalidInput, err)
	}

	return nil
}

func compileSchema(schema []byte) (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", document); err != nil {
		return nil, err
	}

	return compiler.Compile("schema.json")
}
