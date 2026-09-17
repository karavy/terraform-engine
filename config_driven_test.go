package tfengine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeRuleSourcesConflict(t *testing.T) {
	sources := []RuleSource{
		{ID: "a", JSON: []byte(`{"rules":{"vip":{"mappings":{"FGT_X":"spec.x"}}}}`)},
		{ID: "b", JSON: []byte(`{"rules":{"vip":{"mappings":{"FGT_Y":"spec.y"}}}}`)},
	}
	if _, _, err := MergeRuleSources(sources); err == nil {
		t.Fatal("expected error for duplicate rule name across sources, got nil")
	}
}

func TestApplyConfigDrivenRuleSimpleMapping(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "res.tf")
	if err := os.WriteFile(file, []byte(`ip = "<FGT_IP>"`), 0644); err != nil {
		t.Fatal(err)
	}

	rc := RuleMappingConfig{Mappings: map[string]string{"FGT_IP": "spec.address"}}
	spec := map[string]interface{}{"address": "10.0.0.1"}

	if err := applyConfigDrivenRule(rc, "my-cr", spec, file); err != nil {
		t.Fatalf("applyConfigDrivenRule: %v", err)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(out), `ip = "10.0.0.1"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
