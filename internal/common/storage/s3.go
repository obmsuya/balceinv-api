package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type S3Config struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyId     string
	SecretAccessKey string
}

type S3Store struct {
	endpoint        *url.URL
	bucket          string
	region          string
	accessKeyId     string
	secretAccessKey string
	httpClient      *http.Client
}

func NewS3Store(s3Config S3Config) (*S3Store, error) {
	parsedEndpoint, parseError := url.Parse(strings.TrimRight(s3Config.Endpoint, "/"))
	if parseError != nil {
		return nil, fmt.Errorf("failed to parse S3 endpoint: %w", parseError)
	}

	s3Store := &S3Store{
		endpoint:        parsedEndpoint,
		bucket:          s3Config.Bucket,
		region:          s3Config.Region,
		accessKeyId:     s3Config.AccessKeyId,
		secretAccessKey: s3Config.SecretAccessKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	return s3Store, nil
}

func (store *S3Store) Put(ctx context.Context, key string, contentType string, body []byte) error {
	objectResponse, requestError := store.send(ctx, http.MethodPut, key, contentType, body)
	if requestError != nil {
		return requestError
	}
	defer objectResponse.Body.Close()

	isStored := objectResponse.StatusCode == http.StatusOK
	if !isStored {
		return describeFailure("put", objectResponse)
	}

	return nil
}

func (store *S3Store) Get(ctx context.Context, key string) (*Object, error) {
	objectResponse, requestError := store.send(ctx, http.MethodGet, key, "", nil)
	if requestError != nil {
		return nil, requestError
	}
	defer objectResponse.Body.Close()

	if objectResponse.StatusCode == http.StatusNotFound {
		return nil, ErrObjectNotFound
	}
	if objectResponse.StatusCode != http.StatusOK {
		return nil, describeFailure("get", objectResponse)
	}

	objectBody, readError := io.ReadAll(objectResponse.Body)
	if readError != nil {
		return nil, fmt.Errorf("failed to read object body: %w", readError)
	}

	foundObject := &Object{
		Body:        objectBody,
		ContentType: objectResponse.Header.Get("Content-Type"),
	}

	return foundObject, nil
}

func (store *S3Store) Delete(ctx context.Context, key string) error {
	objectResponse, requestError := store.send(ctx, http.MethodDelete, key, "", nil)
	if requestError != nil {
		return requestError
	}
	defer objectResponse.Body.Close()

	isDeleted := objectResponse.StatusCode == http.StatusNoContent || objectResponse.StatusCode == http.StatusOK || objectResponse.StatusCode == http.StatusNotFound
	if !isDeleted {
		return describeFailure("delete", objectResponse)
	}

	return nil
}

func (store *S3Store) send(ctx context.Context, method string, key string, contentType string, body []byte) (*http.Response, error) {
	keyError := ValidateKey(key)
	if keyError != nil {
		return nil, keyError
	}

	requestTime := time.Now().UTC()
	amzDate := requestTime.Format("20060102T150405Z")
	dateStamp := requestTime.Format("20060102")
	payloadDigest := sha256.Sum256(body)
	payloadHash := hex.EncodeToString(payloadDigest[:])
	canonicalPath := "/" + encodePathSegment(store.bucket) + "/" + encodeObjectKey(key)

	requestUrl := *store.endpoint
	requestUrl.Path = canonicalPath
	requestUrl.RawPath = canonicalPath

	signedRequest, buildError := http.NewRequestWithContext(ctx, method, requestUrl.String(), bytes.NewReader(body))
	if buildError != nil {
		return nil, fmt.Errorf("failed to build S3 request: %w", buildError)
	}
	signedRequest.Header.Set("X-Amz-Date", amzDate)
	signedRequest.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if contentType != "" {
		signedRequest.Header.Set("Content-Type", contentType)
	}

	signedHeaderNames := "host;x-amz-content-sha256;x-amz-date"
	canonicalHeaders := "host:" + store.endpoint.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	canonicalRequest := method + "\n" +
		canonicalPath + "\n" +
		"\n" +
		canonicalHeaders + "\n" +
		signedHeaderNames + "\n" +
		payloadHash

	credentialScope := dateStamp + "/" + store.region + "/s3/aws4_request"
	canonicalRequestDigest := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" +
		amzDate + "\n" +
		credentialScope + "\n" +
		hex.EncodeToString(canonicalRequestDigest[:])

	dateKey := hmacSha256([]byte("AWS4"+store.secretAccessKey), dateStamp)
	regionKey := hmacSha256(dateKey, store.region)
	serviceKey := hmacSha256(regionKey, "s3")
	signingKey := hmacSha256(serviceKey, "aws4_request")
	signature := hex.EncodeToString(hmacSha256(signingKey, stringToSign))

	authorizationHeader := "AWS4-HMAC-SHA256 Credential=" + store.accessKeyId + "/" + credentialScope +
		", SignedHeaders=" + signedHeaderNames +
		", Signature=" + signature
	signedRequest.Header.Set("Authorization", authorizationHeader)

	objectResponse, sendError := store.httpClient.Do(signedRequest)
	if sendError != nil {
		return nil, fmt.Errorf("S3 %s %s failed: %w", method, key, sendError)
	}

	return objectResponse, nil
}

func hmacSha256(signingKey []byte, message string) []byte {
	messageMac := hmac.New(sha256.New, signingKey)
	messageMac.Write([]byte(message))
	return messageMac.Sum(nil)
}

func encodeObjectKey(key string) string {
	keySegments := strings.Split(key, "/")
	for segmentIndex, keySegment := range keySegments {
		keySegments[segmentIndex] = encodePathSegment(keySegment)
	}
	return strings.Join(keySegments, "/")
}

func encodePathSegment(segment string) string {
	encodedSegment := strings.Builder{}
	for _, segmentByte := range []byte(segment) {
		isUnreserved := (segmentByte >= 'A' && segmentByte <= 'Z') ||
			(segmentByte >= 'a' && segmentByte <= 'z') ||
			(segmentByte >= '0' && segmentByte <= '9') ||
			segmentByte == '-' || segmentByte == '_' || segmentByte == '.' || segmentByte == '~'
		if isUnreserved {
			encodedSegment.WriteByte(segmentByte)
			continue
		}
		fmt.Fprintf(&encodedSegment, "%%%02X", segmentByte)
	}
	return encodedSegment.String()
}

func describeFailure(operation string, failedResponse *http.Response) error {
	responseSnippet, _ := io.ReadAll(io.LimitReader(failedResponse.Body, 512))
	return fmt.Errorf("S3 %s returned %d: %s", operation, failedResponse.StatusCode, strings.TrimSpace(string(responseSnippet)))
}
