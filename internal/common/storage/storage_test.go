package storage_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/google/uuid"
)

func availableStores(t *testing.T) map[string]storage.Store {
	t.Helper()

	localStore, localError := storage.NewLocalStore(t.TempDir())
	if localError != nil {
		t.Fatalf("local store: %v", localError)
	}
	stores := map[string]storage.Store{
		"local": localStore,
	}

	s3Endpoint, hasS3 := os.LookupEnv("TEST_S3_ENDPOINT")
	if !hasS3 {
		return stores
	}

	s3Store, s3Error := storage.NewS3Store(storage.S3Config{
		Endpoint:        s3Endpoint,
		Bucket:          os.Getenv("TEST_S3_BUCKET"),
		Region:          "garage",
		AccessKeyId:     os.Getenv("TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("TEST_S3_SECRET_ACCESS_KEY"),
	})
	if s3Error != nil {
		t.Fatalf("s3 store: %v", s3Error)
	}
	stores["s3"] = s3Store

	return stores
}

func TestStoresRoundTripAndRejectUnsafeKeys(t *testing.T) {
	for storeName, objectStore := range availableStores(t) {
		t.Run(storeName, func(t *testing.T) {
			testContext := context.Background()
			objectKey := "logos/" + uuid.Must(uuid.NewV7()).String() + "/" + uuid.Must(uuid.NewV7()).String() + ".png"
			objectBody := []byte("\x89PNG\r\n\x1a\nnot-really-a-picture")

			putError := objectStore.Put(testContext, objectKey, "image/png", objectBody)
			if putError != nil {
				t.Fatalf("put: %v", putError)
			}

			storedObject, getError := objectStore.Get(testContext, objectKey)
			if getError != nil {
				t.Fatalf("get: %v", getError)
			}
			if !bytes.Equal(storedObject.Body, objectBody) {
				t.Fatal("stored bytes differ from uploaded bytes")
			}
			if storedObject.ContentType != "image/png" {
				t.Fatalf("content type %q, want image/png", storedObject.ContentType)
			}

			deleteError := objectStore.Delete(testContext, objectKey)
			if deleteError != nil {
				t.Fatalf("delete: %v", deleteError)
			}
			repeatDeleteError := objectStore.Delete(testContext, objectKey)
			if repeatDeleteError != nil {
				t.Fatalf("deleting a missing object must succeed: %v", repeatDeleteError)
			}

			_, missingError := objectStore.Get(testContext, objectKey)
			if !errors.Is(missingError, storage.ErrObjectNotFound) {
				t.Fatalf("get after delete returned %v, want ErrObjectNotFound", missingError)
			}

			unsafeKeys := []string{"../escape.png", "logos/../../etc/passwd", "/absolute.png", "logos//double.png", "Logos/Upper.png", "logos/", ""}
			for _, unsafeKey := range unsafeKeys {
				unsafePutError := objectStore.Put(testContext, unsafeKey, "image/png", objectBody)
				if !errors.Is(unsafePutError, storage.ErrInvalidKey) {
					t.Fatalf("key %q was accepted: %v", unsafeKey, unsafePutError)
				}
			}
		})
	}
}

func TestS3RejectsAWrongSecret(t *testing.T) {
	s3Endpoint, hasS3 := os.LookupEnv("TEST_S3_ENDPOINT")
	if !hasS3 {
		t.Skip("TEST_S3_ENDPOINT not set")
	}

	forgedStore, forgedError := storage.NewS3Store(storage.S3Config{
		Endpoint:        s3Endpoint,
		Bucket:          os.Getenv("TEST_S3_BUCKET"),
		Region:          "garage",
		AccessKeyId:     os.Getenv("TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: "not-the-real-secret",
	})
	if forgedError != nil {
		t.Fatalf("s3 store: %v", forgedError)
	}

	putError := forgedStore.Put(context.Background(), "logos/forged/attempt.png", "image/png", []byte("x"))
	if putError == nil {
		t.Fatal("a request signed with the wrong secret was accepted")
	}
}
