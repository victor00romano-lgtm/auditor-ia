package httpapi

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIYAMLIsValid(t *testing.T) {
	content, err := os.ReadFile("../../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	if document["openapi"] != "3.0.3" || document["paths"] == nil {
		t.Fatalf("OpenAPI incompleto: %#v", document)
	}
}
