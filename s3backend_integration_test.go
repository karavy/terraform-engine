package tfengine_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hashicorp/terraform-exec/tfexec"

	tfengine "github.com/karavy/terraform-engine"
	"github.com/karavy/terraform-engine/s3utils"
)

// TestS3BackendKeepsStateAcrossRuns esegue OpenTofu davvero contro uno storage S3
// compatibile. Richiede TFENGINE_S3_ENDPOINT, TFENGINE_S3_ACCESS_KEY e
// TFENGINE_S3_SECRET_KEY (ad esempio un MinIO locale) e il binario "tofu"; senza,
// il test viene saltato.
//
// Simula due Job diversi: cartelle di lavoro distinte e vuote, stessa chiave di
// state, valori diversi. Il secondo deve trovare e modificare la risorsa creata
// dal primo, non crearne una nuova.
func TestS3BackendKeepsStateAcrossRuns(t *testing.T) {
	endpoint, access, secret := os.Getenv("TFENGINE_S3_ENDPOINT"), os.Getenv("TFENGINE_S3_ACCESS_KEY"), os.Getenv("TFENGINE_S3_SECRET_KEY")
	if endpoint == "" || access == "" || secret == "" {
		t.Skip("TFENGINE_S3_ENDPOINT/ACCESS_KEY/SECRET_KEY non impostate")
	}
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu non trovato nel PATH")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", access)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)
	t.Setenv("TF_IN_AUTOMATION", "true")
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	const bucket, region = "tfengine-test", "us-east-1"
	client := s3utils.NewClient(endpoint, region, access, secret)
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil && !strings.Contains(err.Error(), "BucketAlready") {
		t.Fatalf("creazione del bucket: %v", err)
	}
	key := "tests/" + strings.ReplaceAll(t.Name(), "/", "-") + "/vdc.tfstate"
	backend := tfengine.S3Backend{Endpoint: endpoint, Bucket: bucket, Region: region, Key: key}

	// run è un Job: cartella nuova, template con il valore richiesto, backend generato.
	run := func(value string) {
		t.Helper()
		dir := t.TempDir()
		config := `
resource "terraform_data" "vdc" {
  input = "` + value + `"
}
output "vdc_input" {
  value = terraform_data.vdc.output
}
output "token" {
  value     = "tok-` + value + `"
  sensitive = true
}
`
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := tfengine.WriteS3BackendOverride(dir, backend); err != nil {
			t.Fatal(err)
		}
		tf, err := tfengine.CreateTerraform(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := tfengine.RunInit(tf, ctx); err != nil {
			t.Fatalf("init: %v", err)
		}
		if err := tfengine.RunApply(tf, ctx); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}

	run("one")
	first, err := s3utils.ReadObject(ctx, client, bucket, key)
	if err != nil {
		t.Fatalf("lo state non è stato scritto sullo storage: %v", err)
	}
	if !bytes.Contains(first, []byte(`"terraform_data"`)) {
		t.Fatalf("lo state non contiene la risorsa: %s", first)
	}

	run("two") // un altro Job, una cartella vuota, un valore diverso
	second, err := s3utils.ReadObject(ctx, client, bucket, key)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := tfengine.OutputsFromState(second)
	if err != nil {
		t.Fatal(err)
	}
	document, err := tfengine.OutputsJSON(outputs)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"token":"tok-two","vdc_input":"two"}`; string(document) != want {
		t.Errorf("outputs dopo la seconda esecuzione = %s, want %s", document, want)
	}
	if got := tfengine.SensitiveOutputNames(outputs); len(got) != 1 || got[0] != "token" {
		t.Errorf("output sensibili = %v, want [token]", got)
	}
	// Stessa risorsa, non una nuova: l'id generato dal provider al primo apply resta uguale.
	if firstID, secondID := resourceID(t, first), resourceID(t, second); firstID == "" || firstID != secondID {
		t.Errorf("id della risorsa: prima %q, dopo %q: la seconda esecuzione non ha modificato la risorsa esistente", firstID, secondID)
	}
	if bytes.Equal(first, second) {
		t.Error("lo state non è cambiato dopo la seconda esecuzione")
	}
}

// resourceID restituisce l'id della prima istanza della prima risorsa dello state.
func resourceID(t *testing.T, state []byte) string {
	t.Helper()
	var document struct {
		Resources []struct {
			Instances []struct {
				Attributes struct {
					ID string `json:"id"`
				} `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(state, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Resources) != 1 || len(document.Resources[0].Instances) != 1 {
		t.Fatalf("lo state deve contenere una sola risorsa con una sola istanza: %s", state)
	}
	return document.Resources[0].Instances[0].Attributes.ID
}

// TestS3BackendLocksConcurrentRuns verifica che una seconda esecuzione sulla stessa
// chiave di state fallisca mentre la prima è in corso, invece di sovrapporsi. Stesse
// condizioni di TestS3BackendKeepsStateAcrossRuns.
func TestS3BackendLocksConcurrentRuns(t *testing.T) {
	endpoint, access, secret := os.Getenv("TFENGINE_S3_ENDPOINT"), os.Getenv("TFENGINE_S3_ACCESS_KEY"), os.Getenv("TFENGINE_S3_SECRET_KEY")
	if endpoint == "" || access == "" || secret == "" {
		t.Skip("TFENGINE_S3_ENDPOINT/ACCESS_KEY/SECRET_KEY non impostate")
	}
	if _, err := exec.LookPath("tofu"); err != nil {
		t.Skip("tofu non trovato nel PATH")
	}
	t.Setenv("AWS_ACCESS_KEY_ID", access)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secret)
	t.Setenv("TF_IN_AUTOMATION", "true")
	t.Setenv("HOME", t.TempDir())

	ctx := context.Background()
	const bucket, region = "tfengine-test", "us-east-1"
	client := s3utils.NewClient(endpoint, region, access, secret)
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil && !strings.Contains(err.Error(), "BucketAlready") {
		t.Fatalf("creazione del bucket: %v", err)
	}
	key := "tests/" + strings.ReplaceAll(t.Name(), "/", "-") + "/vdc.tfstate"
	backend := tfengine.S3Backend{Endpoint: endpoint, Bucket: bucket, Region: region, Key: key}

	prepare := func(config string) *tfexec.Terraform {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := tfengine.WriteS3BackendOverride(dir, backend); err != nil {
			t.Fatal(err)
		}
		tf, err := tfengine.CreateTerraform(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := tfengine.RunInit(tf, ctx); err != nil {
			t.Fatalf("init: %v", err)
		}
		return tf
	}
	slow := prepare(`
resource "terraform_data" "vdc" {
  input = "slow"
  provisioner "local-exec" {
    command = "sleep 8"
  }
}
`)
	other := prepare(`
resource "terraform_data" "vdc" {
  input = "other"
}
`)

	slowDone := make(chan error, 1)
	go func() { slowDone <- tfengine.RunApply(slow, ctx) }()

	// Aspetta che il lock sia davvero preso sullo storage.
	locked := false
	for range 100 {
		if _, err := s3utils.ReadObject(ctx, client, bucket, key+".tflock"); err == nil {
			locked = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !locked {
		t.Fatal("il lock dello state non è comparso sullo storage")
	}
	err := tfengine.RunApply(other, ctx)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "lock") {
		t.Fatalf("la seconda esecuzione doveva fallire per il lock, errore = %v", err)
	}
	if err := <-slowDone; err != nil {
		t.Fatalf("la prima esecuzione doveva completarsi: %v", err)
	}
	// A lock rilasciato una nuova esecuzione riesce.
	if err := tfengine.RunApply(other, ctx); err != nil {
		t.Fatalf("dopo il rilascio del lock l'esecuzione doveva riuscire: %v", err)
	}
}
