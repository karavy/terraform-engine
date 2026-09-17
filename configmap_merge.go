package tfengine

// Logica di merge pura, senza dipendenze da nessun client/API esterna -
// una sorgente è già ridotta al minimo indispensabile (un ID per i
// messaggi d'errore + il JSON grezzo), così è testabile con dati Go
// semplici indipendentemente da dove quel JSON viene letto (una ConfigMap
// Kubernetes, un file, un secret manager, ...).
import (
	"encoding/json"
	"fmt"
)

// RuleSource è una sorgente di regole già letta, ridotta al minimo che
// serve al merge: un identificativo (solo per i messaggi d'errore, es.
// "namespace/nome-configmap") e il JSON grezzo (stessa forma di RulesFile:
// "common" e/o "rules").
type RuleSource struct {
	ID   string
	JSON []byte
}

// MergeRuleSources unisce più sorgenti in un unico RulesFile. Regole di
// merge, applicate in modo esplicito e verificabile:
//   - "rules": l'unione di tutte le regole trovate in tutte le sorgenti. Lo
//     STESSO nome di regola definito in più di una sorgente è un errore
//     esplicito (mai una scelta silenziosa su quale vince).
//   - "common": deve essere definita in AL PIÙ UNA sorgente. Se più di una
//     la definisce, è un errore esplicito.
//
// L'ordine di sources non è significativo per il risultato finale (gli
// errori sopra impediscono qualunque dipendenza dall'ordine).
func MergeRuleSources(sources []RuleSource) (RulesFile, string, error) {
	merged := RulesFile{Rules: map[string]RuleMappingConfig{}}
	commonDefinedIn := ""
	used := 0

	for _, src := range sources {
		used++

		var part RulesFile
		if err := json.Unmarshal(src.JSON, &part); err != nil {
			return RulesFile{}, "", fmt.Errorf("parsing della sorgente %s fallito: %w", src.ID, err)
		}

		for name, cfg := range part.Rules {
			if _, exists := merged.Rules[name]; exists {
				return RulesFile{}, "", fmt.Errorf("regola %q definita in più di una sorgente (trovata di nuovo in %s) - ogni regola deve stare in una sola sorgente", name, src.ID)
			}
			merged.Rules[name] = cfg
		}

		if len(part.Common.Mappings) > 0 {
			if commonDefinedIn != "" {
				return RulesFile{}, "", fmt.Errorf("sezione \"common\" definita sia in %s che in %s - deve stare in una sola sorgente", commonDefinedIn, src.ID)
			}
			merged.Common = part.Common
			commonDefinedIn = src.ID
		}
	}

	return merged, fmt.Sprintf("%d sorgenti (su %d fornite)", used, len(sources)), nil
}

// LoadOperatorRulesFromSources unisce sources (vedi MergeRuleSources) e
// registra il ruleset risultante - comodo per chi legge le regole da
// qualunque sistema esterno con "N blob JSON con un ID" (una ConfigMap
// Kubernetes ne è un esempio, ma non l'unico).
func LoadOperatorRulesFromSources(sources []RuleSource) error {
	merged, desc, err := MergeRuleSources(sources)
	if err != nil {
		return err
	}
	return applyRulesFile(merged, desc)
}
