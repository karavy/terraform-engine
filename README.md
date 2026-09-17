# terraform-engine

Motore, generico e indipendente da Kubernetes/CRD, per generare file
Terraform/OpenTofu a partire da template con placeholder e per eseguirli.
Estratto da [k8s-operator-fortigate](https://github.com/karavy/k8s-operator-fortigate)
per essere riusabile anche in altri progetti.

## Cosa offre

- **Template engine** (`RenderTemplate`): sostituisce tag `<FGT_XXX>` in un
  file template contro una qualunque struct/mappa JSON-marshalable, ed
  espande blocchi `<FGT_FOR_STR><CAMPO>...<FGT_FOR_END>` che iterano su una
  lista, eventualmente splittando il template in un file per elemento.
- **Regole config-driven** (`config_driven.go`): descrivi come riempire i
  tag di un template in JSON (`RulesFile`) invece che in codice Go -
  `LoadOperatorRulesFromFile`, `LoadOperatorRulesFromBytes`,
  `LoadOperatorRulesFromDirectory`, `LoadOperatorRulesFromSources`.
- **Registro regole** (`rules.go`): `RegisterOperatorRule`/
  `ApplyOperatorRule` per chi preferisce scrivere una regola a mano invece
  che in JSON.
- **Wrapper Terraform/OpenTofu** (`tfexec.go`): `CreateTerraform`, `RunInit`,
  `NeedsInit` (controllo di allineamento provider via
  `required_providers`/`.terraform.lock.hcl`), `RunApply`.
- **fileutils**, **s3utils**, **configs**: utility generiche di supporto
  (copia/patch di file locali, S3, configurazione dell'engine).

Nessun pacchetto qui dentro importa tipi Kubernetes o un CRD specifico: un
consumer passa sempre `resourceName string` + `spec interface{}` (tipicamente
lo `.Spec` di una custom resource, ma può essere qualunque valore
marshalable in JSON).

## Uso da un altro modulo Go

Finché questo repo non è pubblicato, aggiungilo come dipendenza locale nel
`go.mod` del consumer:

```
require github.com/karavy/terraform-engine v0.0.0

replace github.com/karavy/terraform-engine => ../terraform-engine
```
