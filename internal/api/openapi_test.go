package api

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/query"

	"gopkg.in/yaml.v3"
)

type contract struct {
	Paths map[string]map[string]struct {
		Responses map[string]yaml.Node `yaml:"responses"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]yaml.Node `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func sameFields(t *testing.T, doc contract, schema string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	properties := doc.Components.Schemas[schema].Properties
	if len(properties) != len(fields) {
		t.Errorf("%s schema has %d properties, serialized form has %d", schema, len(properties), len(fields))
	}
	for name := range fields {
		if _, ok := properties[name]; !ok {
			t.Errorf("%s schema missing %s", schema, name)
		}
	}
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
	sameFields(t, doc, "PayoffQuote", query.PayoffQuote{})
	sameFields(t, doc, "ApplicationRequest", ApplicationRequest{})
	sameFields(t, doc, "RepaymentRequest", RepaymentRequest{})
	sameFields(t, doc, "AccrualRequest", AccrualRequest{})
	sameFields(t, doc, "FeeRequest", FeeRequest{})
	sameFields(t, doc, "Agreement", AgreementView{})
	sameFields(t, doc, "ScheduleItem", ScheduleItemView{})
	sameFields(t, doc, "Repayment", RepaymentView{})
	sameFields(t, doc, "Allocation", AllocationView{})
	sameFields(t, doc, "AgreementPayoffQuote", QuoteView{})
	sameFields(t, doc, "ErrorEnvelope", ErrorEnvelope{})
	sameFields(t, doc, "Error", ErrorBody{})

	servicing := []string{"200", "400", "404", "409", "422"}
	cash := []string{"200", "400", "404", "409", "422", "502", "503"}
	want := map[string]map[string][]string{
		"/v1/loans":                   {"post": {"200", "400", "409"}},
		"/v1/loans/{id}/payoff":       {"get": {"200", "400", "404"}},
		"/v1/applications":            {"post": {"200", "400", "409", "422", "502", "503"}},
		"/v1/loans/{id}":              {"get": {"200", "400", "404"}},
		"/v1/loans/{id}/disburse":     {"post": cash},
		"/v1/loans/{id}/repayments":   {"post": cash},
		"/v1/loans/{id}/accruals":     {"post": servicing},
		"/v1/loans/{id}/fees":         {"post": servicing},
		"/v1/loans/{id}/payoff-quote": {"get": {"200", "400", "404", "422"}},
		"/healthz":                    {"get": {"200"}},
		"/readyz":                     {"get": {"200", "503"}},
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
