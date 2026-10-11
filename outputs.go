package tfengine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// OutputValue è un output dichiarato con il blocco "output" nel modulo
// Terraform/OpenTofu eseguito: il suo valore, già codificato in JSON (quindi
// stringa, numero, booleano, lista o oggetto senza perdere il tipo), e il flag
// Sensitive che l'autore del modulo ha impostato con "sensitive = true".
type OutputValue struct {
	Value     json.RawMessage
	Sensitive bool
}

// RunOutput esegue "terraform/tofu output -json" e restituisce gli output
// correnti dello state, per nome. Si usa dopo RunApply. Se lo state non
// contiene output restituisce una mappa vuota, non un errore.
//
// I valori marcati Sensitive sono restituiti in chiaro: decidere dove
// conservarli (log, file, Secret...) resta a carico del chiamante, che può
// riconoscerli con SensitiveOutputNames.
func RunOutput(tf *tfexec.Terraform, ctx context.Context) (map[string]OutputValue, error) {
	raw, err := tf.Output(ctx)
	if err != nil {
		return nil, fmt.Errorf("lettura degli output (terraform output) fallita: %w", err)
	}

	outputs := make(map[string]OutputValue, len(raw))
	for name, meta := range raw {
		outputs[name] = OutputValue{Value: meta.Value, Sensitive: meta.Sensitive}
	}
	return outputs, nil
}

// OutputsJSON serializza outputs come un unico oggetto JSON {"nome": valore, ...}
// con le chiavi in ordine alfabetico. L'oggetto si presta a essere letto con
// qualunque lettore di JSON Pointer, per esempio "/vpc_id" o "/ips/0".
func OutputsJSON(outputs map[string]OutputValue) ([]byte, error) {
	document := make(map[string]json.RawMessage, len(outputs))
	for name, output := range outputs {
		document[name] = output.Value
	}
	return json.Marshal(document)
}

// SensitiveOutputNames restituisce, in ordine alfabetico, i nomi degli output
// marcati Sensitive.
func SensitiveOutputNames(outputs map[string]OutputValue) []string {
	var names []string
	for name, output := range outputs {
		if output.Sensitive {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// OutputsFromState legge gli output dal JSON di uno state Terraform/OpenTofu
// (formato "terraform.tfstate", versione 4), senza eseguire nessun binario: serve a
// chi può leggere lo state dal backend ma non ha accesso al processo che lo ha
// prodotto. Uno state senza output restituisce una mappa vuota.
func OutputsFromState(state []byte) (map[string]OutputValue, error) {
	var document struct {
		Version int `json:"version"`
		Outputs map[string]struct {
			Value     json.RawMessage `json:"value"`
			Sensitive bool            `json:"sensitive"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(state, &document); err != nil {
		return nil, fmt.Errorf("state Terraform non valido: %w", err)
	}
	if document.Version != 4 {
		return nil, fmt.Errorf("versione dello state non supportata: %d (atteso 4)", document.Version)
	}
	outputs := make(map[string]OutputValue, len(document.Outputs))
	for name, output := range document.Outputs {
		outputs[name] = OutputValue{Value: output.Value, Sensitive: output.Sensitive}
	}
	return outputs, nil
}
