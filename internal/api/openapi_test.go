package api

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type contract struct {
	Paths map[string]map[string]struct {
		Responses map[string]yaml.Node `yaml:"responses"`
	} `yaml:"paths"`
}

func TestOpenAPIImplementedRoutesAndStatuses(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc contract
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse OpenAPI: %v", err)
	}
	want := map[string]map[string][]string{
		"/v1/loans":             {"post": {"200", "400", "409"}},
		"/v1/loans/{id}/payoff": {"get": {"200", "400", "404"}},
	}
	if len(doc.Paths) != len(want) {
		t.Fatalf("contract paths = %d, want only %d implemented paths", len(doc.Paths), len(want))
	}
	for path, methods := range want {
		got, ok := doc.Paths[path]
		if !ok {
			t.Fatalf("missing implemented route %s", path)
		}
		if len(got) != len(methods) {
			t.Fatalf("%s methods = %d, want %d", path, len(got), len(methods))
		}
		for method, statuses := range methods {
			resp, ok := got[method]
			if !ok {
				t.Fatalf("missing %s %s", method, path)
			}
			for _, status := range statuses {
				if _, ok := resp.Responses[status]; !ok {
					t.Errorf("missing status %s on %s %s", status, method, path)
				}
			}
			if len(resp.Responses) != len(statuses) {
				t.Errorf("unexpected statuses on %s %s", method, path)
			}
		}
	}
}
