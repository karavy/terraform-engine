package tfengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// BackendOverrideFile è il nome del file generato da WriteS3BackendOverride.
// Terraform/OpenTofu trattano i file "*_override.tf" come override: un blocco
// backend scritto lì sostituisce qualunque backend dichiarato negli altri
// file, quindi un template non può né dimenticare il backend (ritrovandosi con
// uno state locale che si perde) né puntarlo altrove.
const BackendOverrideFile = "backend_override.tf"

// S3Backend descrive dove vive lo state Terraform/OpenTofu: un oggetto in un
// bucket S3 o compatibile (MinIO, Garage, Ceph RGW...). La chiave identifica la
// risorsa gestita: esecuzioni diverse con la stessa chiave lavorano sullo stesso
// state, anche a distanza di tempo e da processi diversi.
//
// Le credenziali non fanno parte della configurazione del backend: OpenTofu le
// legge dall'ambiente (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY).
type S3Backend struct {
	// Endpoint è l'URL dello storage, ad esempio "http://minio.storage:9000".
	Endpoint string
	Bucket   string
	// Region è un valore richiesto dal backend; per storage on-prem è
	// spesso arbitrario (ad esempio "us-east-1" o "garage").
	Region string
	// Key è il percorso dell'oggetto di state dentro il bucket.
	Key string
}

var (
	s3KeyPattern    = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)
	s3BucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	s3RegionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
)

// Validate controlla i campi. Sono valori che finiscono in un file HCL e in
// percorsi di storage, quindi si accettano solo caratteri sicuri: niente "..",
// niente "/" iniziale o finale, niente caratteri di controllo.
func (b S3Backend) Validate() error {
	if !strings.HasPrefix(b.Endpoint, "http://") && !strings.HasPrefix(b.Endpoint, "https://") {
		return errors.New("endpoint S3 non valido: deve iniziare con http:// o https://")
	}
	if strings.ContainsAny(b.Endpoint, " \t\r\n\"\\") {
		return errors.New("endpoint S3 non valido: contiene caratteri non ammessi")
	}
	if !s3BucketPattern.MatchString(b.Bucket) {
		return fmt.Errorf("nome del bucket S3 non valido: %q", b.Bucket)
	}
	if !s3RegionPattern.MatchString(b.Region) {
		return fmt.Errorf("region S3 non valida: %q", b.Region)
	}
	if len(b.Key) > 512 || !s3KeyPattern.MatchString(b.Key) {
		return fmt.Errorf("chiave dello state non valida: %q", b.Key)
	}
	for _, segment := range strings.Split(b.Key, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("chiave dello state non valida: %q", b.Key)
		}
	}
	return nil
}

// WriteS3BackendOverride scrive in dir il file BackendOverrideFile con il backend
// S3 configurato per b e restituisce il suo percorso. Abilita il lock dello state
// con un file di lock sul bucket (use_lockfile, OpenTofu 1.10 o successivo), così
// due esecuzioni sulla stessa chiave non si sovrappongono; richiede che lo storage
// supporti le scritture condizionali.
func WriteS3BackendOverride(dir string, b S3Backend) (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	content := fmt.Sprintf(`terraform {
  backend "s3" {
    bucket                      = %s
    key                         = %s
    region                      = %s
    endpoints                   = { s3 = %s }
    use_path_style              = true
    use_lockfile                = true
    skip_credentials_validation = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
  }
}
`, hclString(b.Bucket), hclString(b.Key), hclString(b.Region), hclString(b.Endpoint))
	path := filepath.Join(dir, BackendOverrideFile)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("scrittura di %s fallita: %w", path, err)
	}
	return path, nil
}

// hclString restituisce s come stringa HCL tra virgolette, neutralizzando le
// sequenze di interpolazione "${" e "%{" che HCL altrimenti valuterebbe.
func hclString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "${", "$${")
	s = strings.ReplaceAll(s, "%{", "%%{")
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}
