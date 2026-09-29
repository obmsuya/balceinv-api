package storage

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
)

func Open(loadedConfig *config.Config) (Store, error) {
	usesObjectStorage := loadedConfig.S3Endpoint != ""
	if usesObjectStorage {
		return NewS3Store(S3Config{
			Endpoint:        loadedConfig.S3Endpoint,
			Bucket:          loadedConfig.S3Bucket,
			Region:          loadedConfig.S3Region,
			AccessKeyId:     loadedConfig.S3AccessKeyId,
			SecretAccessKey: loadedConfig.S3SecretKey,
		})
	}

	hasNoMediaDirectory := loadedConfig.MediaDirectory == ""
	if hasNoMediaDirectory {
		return nil, errors.New("no object storage configured: set S3_ENDPOINT or MEDIA_DIR")
	}

	return NewLocalStore(loadedConfig.MediaDirectory)
}
