package tfengine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRenderTemplateScalarNoLoop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "res.tf")
	if err := os.WriteFile(file, []byte("name = \"<FGT_HOSTNAME>\"\nunresolved = \"<FGT_PORT_NAME>\""), 0644); err != nil {
		t.Fatal(err)
	}

	spec := map[string]interface{}{"hostname": "fw1"}
	if err := RenderTemplate(file, spec); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}

	out, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, `name = "fw1"`) {
		t.Errorf("expected hostname substituted, got: %s", got)
	}
	if !strings.Contains(got, `unresolved = "<FGT_PORT_NAME>"`) {
		t.Errorf("expected unresolved tag left untouched, got: %s", got)
	}
}

func TestRenderTemplateSplitsOnLeadingLoop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.tf.tmpl")
	content := "<FGT_FOR_STR><FIREWALLRULES>rule \"<FGT_FIREWALLRULES_NAME>\" {}\n<FGT_FOR_END>"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	spec := map[string]interface{}{
		"firewallRules": []interface{}{
			map[string]interface{}{"name": "a"},
			map[string]interface{}{"name": "b"},
		},
	}
	if err := RenderTemplate(file, spec); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}

	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("expected original template file to be removed, got err=%v", err)
	}

	for i, want := range []string{"a", "b"} {
		f := filepath.Join(dir, "rules_"+strconv.Itoa(i+1)+".tf")
		out, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("expected generated file %s: %v", f, err)
		}
		if !strings.Contains(string(out), `rule "`+want+`"`) {
			t.Errorf("file %s: expected rule %q, got %s", f, want, out)
		}
	}
}

func TestRenderTemplateLeavesNonTagAngleBracketsAlone(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "res.tf")
	content := "a = var.x <= 5\nb = <<EOT\nhello <FGT_HOSTNAME>\nEOT\nc = \"x => y\"\nd = 1 < 2 && 3 > 2"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RenderTemplate(file, map[string]interface{}{"hostname": "fw1"}); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	out, _ := os.ReadFile(file)
	want := strings.Replace(content, "<FGT_HOSTNAME>", "fw1", 1)
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestRenderTemplateEscapedTagIsLiteral(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "res.tf")
	content := "a = \"\\<FGT_HOSTNAME>\"\nb = \"<FGT_HOSTNAME>\"\nc = \"\\<html>\""
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RenderTemplate(file, map[string]interface{}{"hostname": "fw1"}); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	out, _ := os.ReadFile(file)
	want := "a = \"<FGT_HOSTNAME>\"\nb = \"fw1\"\nc = \"<html>\""
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestRenderTemplateEscapedTagInsideLoop(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.tf.tmpl")
	content := "<FGT_FOR_STR><RULES>r \"<FGT_RULES_NAME>\" \"\\<FGT_NAME>\" x <= 1\n<FGT_FOR_END>"
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	spec := map[string]interface{}{"rules": []interface{}{map[string]interface{}{"name": "a"}}}
	if err := RenderTemplate(file, spec); err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	out, err := os.ReadFile(filepath.Join(dir, "rules_1.tf"))
	if err != nil {
		t.Fatal(err)
	}
	want := "r \"a\" \"<FGT_NAME>\" x <= 1\n"
	if string(out) != want {
		t.Errorf("got %q want %q", out, want)
	}
}
