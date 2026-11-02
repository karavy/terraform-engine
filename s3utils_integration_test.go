package tfengine

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/karavy/terraform-engine/s3utils"
)

// TestS3ObjectsWriteReadDeletePrefix scrive, legge e cancella per prefisso, su uno storage
// S3 reale (come i test del backend): cancellare un prefisso non tocca le chiavi che
// iniziano allo stesso modo senza appartenergli.
func TestS3ObjectsWriteReadDeletePrefix(t *testing.T) {
	endpoint, access, secret := os.Getenv("TFENGINE_S3_ENDPOINT"), os.Getenv("TFENGINE_S3_ACCESS_KEY"), os.Getenv("TFENGINE_S3_SECRET_KEY")
	if endpoint == "" || access == "" || secret == "" {
		t.Skip("TFENGINE_S3_ENDPOINT/ACCESS_KEY/SECRET_KEY non impostate")
	}
	ctx := context.Background()
	const bucket, region = "tfengine-test", "us-east-1"
	client := s3utils.NewClient(endpoint, region, access, secret)
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil && !strings.Contains(err.Error(), "BucketAlready") {
		t.Fatalf("creazione del bucket: %v", err)
	}
	base := "tests/" + t.Name() + "/"
	keys := []string{base + "plan-1/net.tfplan", base + "plan-1/net.tfplan.txt", base + "plan-1/vm/vm.tfplan", base + "plan-10/net.tfplan"}
	for _, key := range keys {
		if err := s3utils.WriteObject(ctx, client, bucket, key, []byte("content of "+key)); err != nil {
			t.Fatal(err)
		}
	}
	data, err := s3utils.ReadObject(ctx, client, bucket, keys[0])
	if err != nil || string(data) != "content of "+keys[0] {
		t.Fatalf("ReadObject = %q, %v", data, err)
	}
	deleted, err := s3utils.DeletePrefix(ctx, client, bucket, base+"plan-1/")
	if err != nil || deleted != 3 {
		t.Fatalf("DeletePrefix = %d, %v; want 3 objects", deleted, err)
	}
	for _, key := range keys[:3] {
		if _, err := s3utils.ReadObject(ctx, client, bucket, key); !errors.Is(err, s3utils.ErrObjectNotFound) {
			t.Errorf("%s must be deleted, got %v", key, err)
		}
	}
	if _, err := s3utils.ReadObject(ctx, client, bucket, keys[3]); err != nil {
		t.Errorf("%s does not belong to the prefix and must stay: %v", keys[3], err)
	}
	if _, err := s3utils.DeletePrefix(ctx, client, bucket, ""); err == nil {
		t.Error("an empty prefix must be refused")
	}
	if _, err := s3utils.DeletePrefix(ctx, client, bucket, base); err != nil {
		t.Fatal(err)
	}
}
