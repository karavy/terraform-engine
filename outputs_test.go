package tfengine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOutputsJSONBuildsOneObjectKeepingTypes(t *testing.T) {
	document, err := OutputsJSON(map[string]OutputValue{
		"vpc_id": {Value: json.RawMessage(`"vpc-1"`)},
		"ips":    {Value: json.RawMessage(`["10.0.0.1","10.0.0.2"]`)},
		"count":  {Value: json.RawMessage(`3`)},
		"nested": {Value: json.RawMessage(`{"a":true}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"count":3,"ips":["10.0.0.1","10.0.0.2"],"nested":{"a":true},"vpc_id":"vpc-1"}`
	if string(document) != want {
		t.Fatalf("document = %s, want %s", document, want)
	}
}

func TestOutputsJSONOfNothingIsAnEmptyObject(t *testing.T) {
	for _, outputs := range []map[string]OutputValue{nil, {}} {
		document, err := OutputsJSON(outputs)
		if err != nil || string(document) != "{}" {
			t.Fatalf("document = %s, err = %v", document, err)
		}
	}
}

func TestSensitiveOutputNamesAreSorted(t *testing.T) {
	names := SensitiveOutputNames(map[string]OutputValue{
		"b": {Sensitive: true}, "a": {Sensitive: true}, "plain": {}, "c": {Sensitive: true},
	})
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Fatalf("names = %v", names)
	}
	if names := SensitiveOutputNames(map[string]OutputValue{"plain": {}}); names != nil {
		t.Fatalf("names = %v, want none", names)
	}
}

// TestRunOutputReadsRealOutputs esegue davvero OpenTofu: usa solo il provider
// integrato terraform_data, quindi non richiede rete né credenziali. Viene
// saltato se "tofu" non è nel PATH.
func TestRunOutputReadsRealOutputs(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu non trovato nel PATH")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TF_IN_AUTOMATION", "true")
	dir := t.TempDir()
	config := `
resource "terraform_data" "vdc" {
  input = "vdc-1"
}
output "vdc_id" {
  value = terraform_data.vdc.output
}
output "ips" {
  value = ["10.0.0.1", "10.0.0.2"]
}
output "password" {
  value     = "s3cr3t"
  sensitive = true
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tf, err := CreateTerraform(dir)
	if err != nil {
		t.Fatal(err)
	}

	before, err := RunOutput(tf, ctx)
	if err != nil || len(before) != 0 {
		t.Fatalf("before any apply: outputs = %v, err = %v", before, err)
	}

	if err := RunInit(tf, ctx); err != nil {
		t.Fatal(err)
	}
	if err := RunApply(tf, ctx); err != nil {
		t.Fatal(err)
	}
	outputs, err := RunOutput(tf, ctx)
	if err != nil {
		t.Fatal(err)
	}

	if got := SensitiveOutputNames(outputs); !reflect.DeepEqual(got, []string{"password"}) {
		t.Errorf("sensitive outputs = %v, want [password]", got)
	}
	document, err := OutputsJSON(outputs)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"ips":["10.0.0.1","10.0.0.2"],"password":"s3cr3t","vdc_id":"vdc-1"}`
	if string(document) != want {
		t.Errorf("document = %s, want %s", document, want)
	}
}

func TestOutputsFromState(t *testing.T) {
	state := `{
  "version": 4,
  "terraform_version": "1.13.1",
  "serial": 7,
  "outputs": {
    "vdc_id":   {"value": "vdc-1", "type": "string"},
    "ips":      {"value": ["10.0.0.1", "10.0.0.2"], "type": ["list", "string"]},
    "password": {"value": "s3cr3t", "type": "string", "sensitive": true}
  },
  "resources": []
}`
	outputs, err := OutputsFromState([]byte(state))
	if err != nil {
		t.Fatal(err)
	}
	document, err := OutputsJSON(outputs)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"ips":["10.0.0.1","10.0.0.2"],"password":"s3cr3t","vdc_id":"vdc-1"}`; string(document) != want {
		t.Errorf("document = %s, want %s", document, want)
	}
	if got := SensitiveOutputNames(outputs); !reflect.DeepEqual(got, []string{"password"}) {
		t.Errorf("sensitive = %v", got)
	}
}

func TestOutputsFromStateEdgeCases(t *testing.T) {
	if outputs, err := OutputsFromState([]byte(`{"version":4,"resources":[]}`)); err != nil || len(outputs) != 0 {
		t.Errorf("state without outputs: %v %v", outputs, err)
	}
	for name, state := range map[string]string{
		"not JSON":        `not json`,
		"wrong version":   `{"version":3,"outputs":{}}`,
		"missing version": `{"outputs":{}}`,
	} {
		if _, err := OutputsFromState([]byte(state)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
