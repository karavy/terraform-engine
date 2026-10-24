package tfengine

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/hashicorp/terraform-exec/tfexec"
	tfjson "github.com/hashicorp/terraform-json"
)

// PlanSummary riassume un plan salvato: quante risorse crea, modifica e
// distrugge. Una risorsa sostituita (destroy + create) conta sia in Add sia
// in Destroy, come nel riepilogo di "terraform plan". HasChanges è vero anche
// quando cambiano solo gli output.
type PlanSummary struct {
	HasChanges bool `json:"hasChanges"`
	Add        int  `json:"add"`
	Change     int  `json:"change"`
	Destroy    int  `json:"destroy"`
}

// SummarizePlan conta le azioni sulle risorse di un plan in formato JSON
// (l'output di "terraform show -json <plan>"). Le letture (data source) e le
// risorse senza modifiche non contano. HasChanges non è calcolato qui: lo
// decide il plan stesso (vedi RunPlan).
func SummarizePlan(plan *tfjson.Plan) PlanSummary {
	var summary PlanSummary
	if plan == nil {
		return summary
	}
	for _, change := range plan.ResourceChanges {
		if change == nil || change.Change == nil {
			continue
		}
		actions := change.Change.Actions
		switch {
		case actions.Replace():
			summary.Add++
			summary.Destroy++
		case actions.Create():
			summary.Add++
		case actions.Update():
			summary.Change++
		case actions.Delete():
			summary.Destroy++
		}
	}
	return summary
}

// RunPlan esegue "terraform/tofu plan" salvando il plan in planFile, senza
// applicarlo, e ne restituisce il riepilogo e il testo leggibile (quello di
// "terraform show <plan>"), da far approvare prima di RunApplyPlan.
//
// La lettura del plan salvato non viene scritta sullo stdout del client:
// la versione JSON contiene i valori delle risorse, compresi quelli
// sensibili. Al termine lo stdout torna os.Stdout, come in CreateTerraform.
func RunPlan(tf *tfexec.Terraform, ctx context.Context, planFile string) (PlanSummary, string, error) {
	fmt.Println("📋 Esecuzione di terraform plan (senza apply)...")
	changes, err := tf.Plan(ctx, tfexec.Out(planFile))
	if err != nil {
		return PlanSummary{}, "", fmt.Errorf("terraform plan: %w", err)
	}
	tf.SetStdout(io.Discard)
	defer tf.SetStdout(os.Stdout)
	plan, err := tf.ShowPlanFile(ctx, planFile)
	if err != nil {
		return PlanSummary{}, "", fmt.Errorf("lettura del plan salvato: %w", err)
	}
	text, err := tf.ShowPlanFileRaw(ctx, planFile)
	if err != nil {
		return PlanSummary{}, "", fmt.Errorf("lettura del testo del plan salvato: %w", err)
	}
	summary := SummarizePlan(plan)
	summary.HasChanges = changes
	if changes {
		fmt.Printf("🔍 Plan salvato: %d da aggiungere, %d da modificare, %d da distruggere.\n", summary.Add, summary.Change, summary.Destroy)
	} else {
		fmt.Println("😎 Nessuna modifica: l'infrastruttura corrisponde alla configurazione.")
	}
	return summary, text, nil
}

// RunApplyPlan applica esattamente il plan salvato in planFile, senza
// calcolarne uno nuovo. Se lo state è cambiato dopo il plan, terraform lo
// rifiuta come "stale" e non modifica nulla.
func RunApplyPlan(tf *tfexec.Terraform, ctx context.Context, planFile string) error {
	fmt.Println("🛠️ Applicazione del plan approvato (terraform apply <plan>)...")
	if err := tf.Apply(ctx, tfexec.DirOrPlan(planFile)); err != nil {
		return fmt.Errorf("terraform apply del plan salvato: %w", err)
	}
	fmt.Println("✅ Plan applicato con successo!")
	return nil
}
