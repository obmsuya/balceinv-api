package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
)

type LocalStore struct {
	rootDirectory string
}

func NewLocalStore(rootDirectory string) (*LocalStore, error) {
	createDirectoryError := os.MkdirAll(rootDirectory, 0o700)
	if createDirectoryError != nil {
		return nil, fmt.Errorf("failed to create media directory: %w", createDirectoryError)
	}

	localStore := &LocalStore{
		rootDirectory: rootDirectory,
	}

	return localStore, nil
}

func (store *LocalStore) Put(ctx context.Context, key string, contentType string, body []byte) error {
	keyError := ValidateKey(key)
	if keyError != nil {
		return keyError
	}

	objectPath := store.pathFor(key)
	createDirectoryError := os.MkdirAll(filepath.Dir(objectPath), 0o700)
	if createDirectoryError != nil {
		return fmt.Errorf("failed to create object folder: %w", createDirectoryError)
	}

	temporaryPath := objectPath + ".uploading"
	writeError := os.WriteFile(temporaryPath, body, 0o600)
	if writeError != nil {
		return fmt.Errorf("failed to write object: %w", writeError)
	}

	renameError := os.Rename(temporaryPath, objectPath)
	if renameError != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("failed to finish writing object: %w", renameError)
	}

	return nil
}

func (store *LocalStore) Get(ctx context.Context, key string) (*Object, error) {
	keyError := ValidateKey(key)
	if keyError != nil {
		return nil, keyError
	}

	objectBody, readError := os.ReadFile(store.pathFor(key))
	if errors.Is(readError, fs.ErrNotExist) {
		return nil, ErrObjectNotFound
	}
	if readError != nil {
		return nil, fmt.Errorf("failed to read object: %w", readError)
	}

	foundObject := &Object{
		Body:        objectBody,
		ContentType: mime.TypeByExtension(filepath.Ext(key)),
	}

	return foundObject, nil
}

func (store *LocalStore) Delete(ctx context.Context, key string) error {
	keyError := ValidateKey(key)
	if keyError != nil {
		return keyError
	}

	removeError := os.Remove(store.pathFor(key))
	isAlreadyGone := errors.Is(removeError, fs.ErrNotExist)
	if removeError != nil && !isAlreadyGone {
		return fmt.Errorf("failed to delete object: %w", removeError)
	}

	return nil
}

func (store *LocalStore) pathFor(key string) string {
	return filepath.Join(store.rootDirectory, filepath.FromSlash(key))
}
