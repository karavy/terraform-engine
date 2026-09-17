package tfengine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/terraform-exec/tfexec"
)

// CreateTerraform crea un client tfexec.Terraform per workingDir, usando il
// binario "tofu" trovato nel PATH, e ne reindirizza stdout/stderr sulla
// console del processo Go corrente (così i log di Terraform/Tofu restano
// visibili in tempo reale).
func CreateTerraform(workingDir string) (*tfexec.Terraform, error) {
	execPath, err := exec.LookPath("tofu")
	if err != nil {
		fmt.Printf("Terraform (tofu) non trovato nel PATH: %v", err)
		return nil, err
	}

	tf, err := tfexec.NewTerraform(workingDir, execPath)
	if err != nil {
		fmt.Printf("Impossibile inizializzare tfexec: %v", err)
		return nil, err
	}

	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	return tf, nil
}

// RunInit esegue "terraform/tofu init" con le opzioni standard di questo
// motore (ForceCopy + Upgrade). Le variabili d'ambiente che init si
// aspetta (credenziali, TF_DATA_DIR, TF_PLUGIN_CACHE_DIR, ...) restano a
// carico del chiamante, che ne conosce i percorsi/valori reali.
func RunInit(tf *tfexec.Terraform, ctx context.Context) error {
	return tf.Init(ctx, tfexec.ForceCopy(true), tfexec.Upgrade(true))
}

// NeedsInit decide se "terraform/tofu init" va rieseguito, guardando (in
// ordine):
//  1. se lockFilePath non esiste ancora -> init necessario (prima
//     esecuzione per questo working dir).
//  2. altrimenti, se il provider richiesto in mainTfPath (blocco
//     required_providers) non è allineato con quello effettivamente
//     pinnato in lockFilePath -> init necessario per aggiornare il lock.
func NeedsInit(mainTfPath, lockFilePath string) (bool, error) {
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		return true, nil
	}

	aligned, err := IsProviderAligned(mainTfPath, lockFilePath)
	if err != nil {
		return false, err
	}
	return !aligned, nil
}

// RunApply esegue "terraform/tofu plan" e, se vengono rilevate modifiche,
// "apply".
func RunApply(tf *tfexec.Terraform, ctx context.Context) error {
	fmt.Println("📋 Esecuzione di terraform plan...")
	changes, err := tf.Plan(ctx)
	if err != nil {
		fmt.Printf("Errore durante terraform plan: %v", err)
		return err
	}

	if changes {
		fmt.Println("🔍 Sono state rilevate modifiche infrastrutturali!")

		fmt.Println("🛠️ Applicazione delle modifiche (terraform apply)...")
		if err := tf.Apply(ctx); err != nil {
			fmt.Printf("Errore durante terraform apply: %v", err)
			return err
		}
		fmt.Println("✅ Infrastruttura aggiornata con successo!")
	} else {
		fmt.Println("😎 Nessuna modifica da applicare, l'infrastruttura è già aggiornata.")
	}

	return nil
}

// --- Parsing del common.tf (required_providers) ---

type RequiredProvider struct {
	Source  string
	Version string
}

func parseRequiredProviders(path string) (map[string]RequiredProvider, error) {
	parser := hclparse.NewParser()
	f, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, diags
	}

	result := make(map[string]RequiredProvider)

	// content del blocco "terraform"
	rootContent, _, diags := f.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: "terraform"}},
	})
	if diags.HasErrors() {
		return nil, diags
	}

	for _, tfBlock := range rootContent.Blocks {
		// dentro "terraform", cerco "required_providers"
		tfContent, _, diags := tfBlock.Body.PartialContent(&hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{{Type: "required_providers"}},
		})
		if diags.HasErrors() {
			return nil, diags
		}

		for _, rpBlock := range tfContent.Blocks {
			// required_providers non ha schema fisso (i nomi provider sono dinamici)
			// quindi uso JustAttributes
			attrs, diags := rpBlock.Body.JustAttributes()
			if diags.HasErrors() {
				return nil, diags
			}

			for providerName, attr := range attrs {
				val, diags := attr.Expr.Value(nil)
				if diags.HasErrors() {
					return nil, diags
				}
				if !val.Type().IsObjectType() {
					continue
				}

				rp := RequiredProvider{}
				valMap := val.AsValueMap()
				if s, ok := valMap["source"]; ok {
					rp.Source = s.AsString()
				}
				if v, ok := valMap["version"]; ok {
					rp.Version = v.AsString()
				}
				result[providerName] = rp
			}
		}
	}

	return result, nil
}

// --- Parsing del lock file (.terraform.lock.hcl) ---

type ProviderLock struct {
	Source  string
	Version string
}

func parseLockFile(path string) (map[string]ProviderLock, error) {
	parser := hclparse.NewParser()
	f, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, diags
	}

	locks := make(map[string]ProviderLock)

	content, _, diags := f.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "provider", LabelNames: []string{"source"}},
		},
	})
	if diags.HasErrors() {
		return nil, diags
	}

	for _, block := range content.Blocks {
		source := block.Labels[0]

		bContent, _, diags := block.Body.PartialContent(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{{Name: "version"}},
		})
		if diags.HasErrors() {
			return nil, diags
		}

		var version string
		if attr, ok := bContent.Attributes["version"]; ok {
			val, diags := attr.Expr.Value(nil)
			if diags.HasErrors() {
				return nil, diags
			}
			version = val.AsString()
		}

		locks[source] = ProviderLock{Source: source, Version: version}
	}

	return locks, nil
}

// --- Confronto ---

// IsProviderAligned confronta i provider richiesti nel blocco
// required_providers di mainTfPath con quelli effettivamente pinnati in
// lockFilePath (.terraform.lock.hcl), ignorando l'host del registry (es.
// registry.opentofu.org vs registry.terraform.io) e confrontando solo sul
// suffisso "<namespace>/<tipo>".
func IsProviderAligned(mainTfPath, lockFilePath string) (bool, error) {
	required, err := parseRequiredProviders(mainTfPath)
	if err != nil {
		return false, fmt.Errorf("errore parsing common: %w", err)
	}

	locked, err := parseLockFile(lockFilePath)
	if err != nil {
		return false, fmt.Errorf("errore parsing lock file: %w", err)
	}

	for name, req := range required {
		found := false
		for lockSource, lock := range locked {
			// match sul suffisso, ignora l'host del registry
			// (registry.opentofu.org vs registry.terraform.io)
			if strings.HasSuffix(lockSource, "/"+req.Source) {
				found = true
				if lock.Version != req.Version {
					return false, nil // versione richiesta diversa da quella pinnata
				}
			}
		}
		if !found {
			return false, nil // provider mai inizializzato
		}
		_ = name
	}

	return true, nil
}
