package tfengine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

func TestSummarizePlanCountsResourceActions(t *testing.T) {
	change := func(actions ...tfjson.Action) *tfjson.ResourceChange {
		return &tfjson.ResourceChange{Change: &tfjson.Change{Actions: actions}}
	}
	plan := &tfjson.Plan{ResourceChanges: []*tfjson.ResourceChange{
		change(tfjson.ActionCreate),
		change(tfjson.ActionCreate),
		change(tfjson.ActionUpdate),
		change(tfjson.ActionDelete),
		change(tfjson.ActionDelete, tfjson.ActionCreate), // sostituzione
		change(tfjson.ActionCreate, tfjson.ActionDelete), // sostituzione create-before-destroy
		change(tfjson.ActionNoop),
		change(tfjson.ActionRead),
		nil,
		{Change: nil},
	}}
	got := SummarizePlan(plan)
	if want := (PlanSummary{Add: 4, Change: 1, Destroy: 3}); got != want {
		t.Fatalf("SummarizePlan = %+v, want %+v", got, want)
	}
	if got := SummarizePlan(nil); got != (PlanSummary{}) {
		t.Fatalf("SummarizePlan(nil) = %+v", got)
	}
}

// TestRunPlanThenApplyPlan usa il vero binario tofu con una risorsa
// terraform_data (nessun provider da scaricare): il plan salvato viene
// applicato così com'è, e un plan reso vecchio da un altro apply è rifiutato.
func TestRunPlanThenApplyPlan(t *testing.T) {
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu non è nel PATH")
	}
	ctx := context.Background()
	dir := t.TempDir()
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`resource "terraform_data" "a" { input = "uno" }`)
	tf, err := CreateTerraform(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := RunInit(tf, ctx); err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(dir, "first.tfplan")
	summary, text, err := RunPlan(tf, ctx, planFile)
	if err != nil {
		t.Fatal(err)
	}
	if want := (PlanSummary{HasChanges: true, Add: 1}); summary != want {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}
	if !strings.Contains(text, "terraform_data.a") || strings.Contains(text, "\x1b[") {
		t.Fatalf("il testo del plan deve nominare la risorsa, senza colori:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, "terraform.tfstate")); err == nil {
		t.Fatal("RunPlan non deve applicare nulla")
	}

	if err := RunApplyPlan(tf, ctx, planFile); err != nil {
		t.Fatal(err)
	}
	summary, _, err = RunPlan(tf, ctx, filepath.Join(dir, "second.tfplan"))
	if err != nil || summary.HasChanges {
		t.Fatalf("dopo l'apply non devono restare modifiche: %+v, %v", summary, err)
	}

	write(`resource "terraform_data" "a" { input = "due" }`)
	stale := filepath.Join(dir, "stale.tfplan")
	if summary, _, err = RunPlan(tf, ctx, stale); err != nil || summary != (PlanSummary{HasChanges: true, Change: 1}) {
		t.Fatalf("cambiare input modifica terraform_data: %+v, %v", summary, err)
	}
	newer := filepath.Join(dir, "newer.tfplan")
	if _, _, err := RunPlan(tf, ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := RunApplyPlan(tf, ctx, newer); err != nil {
		t.Fatal(err)
	}
	if err := RunApplyPlan(tf, ctx, stale); err == nil {
		t.Fatal("un plan calcolato prima di un altro apply deve essere rifiutato")
	}
}
