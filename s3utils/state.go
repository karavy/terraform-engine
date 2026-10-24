package s3utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// MaxObjectBytes limita la lettura di un singolo oggetto con ReadObject.
const MaxObjectBytes = 16 << 20

// ErrObjectNotFound è restituito da ReadObject quando la chiave non esiste.
var ErrObjectNotFound = errors.New("oggetto S3 non trovato")

// NewClient crea un client S3 per uno storage compatibile (MinIO, Garage, Ceph RGW),
// con routing path-style. A differenza di CreateS3Client, la region è un parametro
// esplicito e non dipende dalla configurazione globale dell'operator.
func NewClient(endpoint, region, accessKeyID, secretAccessKey string) *s3.Client {
	return s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Region:       region,
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
	})
}

// ReadObject legge un oggetto intero, fino a MaxObjectBytes. Se la chiave non esiste
// restituisce ErrObjectNotFound (incapsulato), così il chiamante può distinguere
// "nessuno state ancora" da un errore di accesso.
func ReadObject(ctx context.Context, client *s3.Client, bucket, key string) ([]byte, error) {
	output, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var missing *types.NoSuchKey
		if errors.As(err, &missing) {
			return nil, fmt.Errorf("%w: %s/%s", ErrObjectNotFound, bucket, key)
		}
		return nil, fmt.Errorf("lettura di %s/%s fallita: %w", bucket, key, err)
	}
	defer func() { _ = output.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(output.Body, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("lettura del contenuto di %s/%s fallita: %w", bucket, key, err)
	}
	if len(data) > MaxObjectBytes {
		return nil, fmt.Errorf("l'oggetto %s/%s supera %d byte", bucket, key, MaxObjectBytes)
	}
	return data, nil
}

// WriteObject scrive un oggetto intero, sovrascrivendolo se esiste già.
func WriteObject(ctx context.Context, client *s3.Client, bucket, key string, data []byte) error {
	if _, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(data)}); err != nil {
		return fmt.Errorf("scrittura di %s/%s fallita: %w", bucket, key, err)
	}
	return nil
}
