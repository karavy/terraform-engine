package tfengine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	fileutils "github.com/karavy/terraform-engine/fileutils"
)

// SplitTerraformExt calcola (base, ext) garantendo che ext sia SEMPRE
// l'estensione Terraform reale (tipicamente ".tf"), anche se filename
// arriva ancora con il suffisso ".tmpl" del file sorgente del template
// (es. "myfw_firewall_policy.tf.tmpl"). Serve perché Tofu/Terraform
// ignorano silenziosamente qualunque file che non finisca in ".tf" (o
// ".tf.json") - un file generato come "..._1.tmpl" (o peggio,
// "...tf_1.tmpl", con ".tf" finito in mezzo al nome) semplicemente non
// viene applicato, senza errore.
//
// Unico punto di verità per il naming dei file splittati/rinominati: usata
// da IndexedResourceFile, dal motore di rendering dei template
// (template.go) e da ApplyCommonMods.
func SplitTerraformExt(filename string) (base, ext string) {
	if before, ok := strings.CutSuffix(filename, ".tmpl"); ok {
		filename = before
	}
	ext = filepath.Ext(filename)
	if ext == "" {
		// Nessuna estensione riconoscibile nemmeno dopo aver tolto
		// ".tmpl": garantiamo comunque ".tf", dato che questo motore
		// genera sempre file Terraform.
		return filename, ".tf"
	}
	base = strings.TrimSuffix(filename, ext)
	return base, ext
}

// IndexedResourceFile returns the Nth (0-based index in, 1-based suffix
// out) generated Terraform file path for a rule whose CR contains a list
// (one CR -> many files), e.g. baseFile "wd/myfw_fwrules.tf" and index 0 ->
// "wd/myfw_fwrules_1.tf". Sempre con estensione ".tf" reale, anche se
// baseFile arriva come "wd/myfw_fwrules.tf.tmpl" - vedi SplitTerraformExt.
//
// IMPORTANT: this must match whatever naming convention RenderTemplate
// actually gives the files it splits out from a file-level <FGT_FOR_STR>
// loop (vedi splitIntoFiles in template.go, che usa la stessa
// SplitTerraformExt) - ogni regola a lista passa da qui, quindi c'è un
// solo posto da correggere.
func IndexedResourceFile(baseFile string, index int) string {
	base, ext := SplitTerraformExt(baseFile)
	return fmt.Sprintf("%s_%d%s", base, index+1, ext)
}

// applyMods runs fileutils.ModifyFileValue against path and wraps any
// error with which file it belongs to, so callers don't each need their
// own fmt.Printf("Errore durante la modifica dei file: %v\n", err) copy.
func applyMods(path string, mods map[string]string) error {
	if err := fileutils.ModifyFileValue(path, mods); err != nil {
		return fmt.Errorf("modifica di %s fallita: %w", path, err)
	}
	return nil
}

// ApplyCommonMods sostituisce mods dentro filename (tipicamente i tag
// "comuni" applicati a ogni CR - provider/backend Terraform) e garantisce
// che il file finale su disco finisca sempre in ".tf" (stessa garanzia di
// SplitTerraformExt), anche se filename arriva ancora con ".tmpl" nel nome.
// Restituisce il percorso finale su cui sono state applicate le modifiche.
func ApplyCommonMods(filename string, mods map[string]string) (string, error) {
	// modifyFileValue legge E scrive su "filename" così com'è - deve
	// restare il path REALE su cui il file è stato copiato (che può
	// ancora avere ".tmpl" nel nome, es. "..._00_common.tf.tmpl"). Non
	// tocchiamo filename PRIMA di questa chiamata: fallirebbe in lettura
	// cercando un file che non esiste con un nome diverso da quello reale
	// su disco.
	if err := applyMods(filename, mods); err != nil {
		return "", err
	}

	// SOLO ORA, a sostituzione avvenuta, garantiamo che il file finale
	// (quello che Tofu/Terraform vedrà davvero) finisca sempre in ".tf".
	base, ext := SplitTerraformExt(filename)
	finalName := base + ext
	if finalName != filename {
		if err := os.Rename(filename, finalName); err != nil {
			return "", fmt.Errorf("rinomina di %s in %s fallita: %w", filename, finalName, err)
		}
	}

	return finalName, nil
}
