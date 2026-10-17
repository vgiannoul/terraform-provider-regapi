package fakeapi_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vgiannoul/terraform-provider-regapi/internal/client"
	"github.com/vgiannoul/terraform-provider-regapi/internal/fakeapi"
)

type spec struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type schema struct {
	Ref        string            `yaml:"$ref"`
	Properties map[string]schema `yaml:"properties"`
	AllOf      []schema          `yaml:"allOf"`
}

func loadSpec(t *testing.T) spec {
	t.Helper()
	raw, err := os.ReadFile("testdata/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var s spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// properties lists the property names of a schema, following allOf and $ref.
func (s spec) properties(sc schema, into map[string]bool) {
	if sc.Ref != "" {
		s.properties(s.Components.Schemas[strings.TrimPrefix(sc.Ref, "#/components/schemas/")], into)
	}
	for _, part := range sc.AllOf {
		s.properties(part, into)
	}
	for name := range sc.Properties {
		into[name] = true
	}
}

func TestEveryRouteTheFakeServesIsInTheAPIDescription(t *testing.T) {
	s := loadSpec(t)
	for _, route := range fakeapi.New().Routes() {
		method, path, _ := strings.Cut(route, " ")
		if _, ok := s.Paths[path][strings.ToLower(method)]; !ok {
			t.Errorf("%s is not in openapi.yaml", route)
		}
	}
}

func TestEveryFieldTheClientReadsIsInTheAPIDescription(t *testing.T) {
	s := loadSpec(t)
	for schemaName, typ := range map[string]reflect.Type{
		"Contact": reflect.TypeFor[client.Contact](), "Domain": reflect.TypeFor[client.Domain](),
		"Order": reflect.TypeFor[client.Order](), "CheckResult": reflect.TypeFor[client.CheckResult](),
		"Price": reflect.TypeFor[client.Price](),
	} {
		props := map[string]bool{}
		s.properties(schema{Ref: "#/components/schemas/" + schemaName}, props)
		for i := range typ.NumField() {
			tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
			if tag != "" && !props[tag] {
				t.Errorf("%s.%s (%q) is not in the %s schema", typ.Name(), typ.Field(i).Name,
					tag, schemaName)
			}
		}
	}
}
