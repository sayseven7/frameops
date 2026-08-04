// Package objectstore stores evidence bytes in one S3-compatible bucket.
//
// The adapter speaks the small part of the S3 API FrameOPS needs, signed with
// AWS Signature Version 4, instead of adopting a full SDK for three requests.
// Every upload is signed over the digest of its own payload, so the object store
// itself refuses bytes that do not match the digest the server recorded.
package objectstore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	signatureAlgorithm = "AWS4-HMAC-SHA256"
	storageService     = "s3"
	defaultRegion      = "us-east-1"
	requestTimeout     = 2 * time.Minute
	// emptyPayloadHash is the SHA-256 of zero bytes, required on requests that
	// carry no body.
	emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type Bucket struct {
	endpoint      *url.URL
	name          string
	region        string
	accessKey     string
	secretKey     string
	retentionDays int
	client        *http.Client
}

// FromEnv reads the evidence bucket configuration from the environment. The API
// refuses to start without it: evidence capture has no degraded mode.
func FromEnv() (Bucket, error) {
	bucket, err := New(
		os.Getenv("FRAMEOPS_EVIDENCE_S3_ENDPOINT"),
		os.Getenv("FRAMEOPS_EVIDENCE_S3_BUCKET"),
		os.Getenv("FRAMEOPS_EVIDENCE_S3_REGION"),
		os.Getenv("FRAMEOPS_EVIDENCE_S3_ACCESS_KEY"),
		os.Getenv("FRAMEOPS_EVIDENCE_S3_SECRET_KEY"),
	)
	if err != nil {
		return Bucket{}, err
	}
	retentionDays, err := strconv.Atoi(os.Getenv("FRAMEOPS_OBJECT_RETENTION_DAYS"))
	if err != nil || retentionDays < 1 {
		return Bucket{}, errors.New("FRAMEOPS_OBJECT_RETENTION_DAYS must be a positive whole number")
	}
	bucket.retentionDays = retentionDays
	return bucket, nil
}

func New(endpoint, name, region, accessKey, secretKey string) (Bucket, error) {
	if endpoint == "" || name == "" || accessKey == "" || secretKey == "" {
		return Bucket{}, errors.New("evidence object storage endpoint, bucket, access key, and secret key must all be set")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return Bucket{}, errors.New("evidence object storage endpoint must be an http or https URL")
	}
	if !validBucketName(name) {
		return Bucket{}, errors.New("evidence bucket name must be 3 to 63 lowercase letters, digits, or inner hyphens")
	}
	if region == "" {
		region = defaultRegion
	}
	return Bucket{
		endpoint:  &url.URL{Scheme: parsed.Scheme, Host: parsed.Host},
		name:      name,
		region:    region,
		accessKey: accessKey,
		secretKey: secretKey,
		client:    &http.Client{Timeout: requestTimeout},
	}, nil
}

// EnsureBucket creates the evidence bucket when it does not exist yet, so a
// self-hosted deployment fails at start-up rather than at the first capture.
func (bucket Bucket) EnsureBucket(ctx context.Context) error {
	if bucket.retentionDays < 1 {
		return errors.New("evidence object storage requires a positive retention period")
	}
	response, err := bucket.send(ctx, http.MethodPut, "", "", nil, 0, emptyPayloadHash, http.Header{"X-Amz-Bucket-Object-Lock-Enabled": []string{"true"}})
	if err != nil {
		return fmt.Errorf("create evidence bucket: %w", err)
	}
	defer drain(response)
	// An existing bucket this account already owns is the expected steady state.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusConflict {
		return fmt.Errorf("create evidence bucket: object storage answered %s", response.Status)
	}
	if response.StatusCode == http.StatusOK || response.StatusCode == http.StatusConflict {
		if err := bucket.configureObjectLock(ctx); err != nil {
			return err
		}
	}
	return bucket.verifyObjectLock(ctx)
}

// Ready verifies configured compliance retention without changing bucket state.
func (bucket Bucket) Ready(ctx context.Context) error {
	if bucket.retentionDays < 1 {
		return errors.New("evidence object storage requires a positive retention period")
	}
	return bucket.verifyObjectLock(ctx)
}

// Put writes exactly size bytes under key. payloadSHA256 is the hex digest the
// server computed over those bytes; the object store recomputes it and rejects
// the request when the body it received differs, so a truncated or altered
// upload can never be recorded as stored evidence.
func (bucket Bucket) Put(ctx context.Context, key string, body io.Reader, size int64, payloadSHA256, contentType string) error {
	if bucket.retentionDays < 1 {
		return errors.New("evidence object storage requires a positive retention period")
	}
	headers := http.Header{
		"Content-Type":                        []string{contentType},
		"If-None-Match":                       []string{"*"},
		"X-Amz-Object-Lock-Mode":              []string{"COMPLIANCE"},
		"X-Amz-Object-Lock-Retain-Until-Date": []string{time.Now().UTC().AddDate(0, 0, bucket.retentionDays).Format(time.RFC3339)},
	}
	response, err := bucket.send(ctx, http.MethodPut, key, "", body, size, payloadSHA256, headers)
	if err != nil {
		return fmt.Errorf("put evidence object: %w", err)
	}
	defer drain(response)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("put evidence object: object storage answered %s", response.Status)
	}
	return nil
}

// Get reads one stored object. The caller closes the returned reader.
func (bucket Bucket) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	response, err := bucket.send(ctx, http.MethodGet, key, "", nil, 0, emptyPayloadHash, nil)
	if err != nil {
		return nil, fmt.Errorf("get evidence object: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		drain(response)
		return nil, fmt.Errorf("get evidence object: object storage answered %s", response.Status)
	}
	return response.Body, nil
}

func (bucket Bucket) configureObjectLock(ctx context.Context) error {
	body := []byte(fmt.Sprintf(`<ObjectLockConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ObjectLockEnabled>Enabled</ObjectLockEnabled><Rule><DefaultRetention><Mode>COMPLIANCE</Mode><Days>%d</Days></DefaultRetention></Rule></ObjectLockConfiguration>`, bucket.retentionDays))
	payloadSHA256 := hex.EncodeToString(digestOf(string(body)))
	response, err := bucket.send(ctx, http.MethodPut, "", "object-lock=", strings.NewReader(string(body)), int64(len(body)), payloadSHA256, http.Header{"Content-Type": []string{"application/xml"}})
	if err != nil {
		return fmt.Errorf("configure evidence object retention: %w", err)
	}
	defer drain(response)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("configure evidence object retention: object storage answered %s", response.Status)
	}
	return nil
}

func (bucket Bucket) verifyObjectLock(ctx context.Context) error {
	response, err := bucket.send(ctx, http.MethodGet, "", "object-lock=", nil, 0, emptyPayloadHash, nil)
	if err != nil {
		return fmt.Errorf("verify evidence object retention: %w", err)
	}
	defer drain(response)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("verify evidence object retention: object storage answered %s", response.Status)
	}
	var configuration struct {
		Enabled string `xml:"ObjectLockEnabled"`
		Rule    struct {
			Retention struct {
				Mode string `xml:"Mode"`
				Days int    `xml:"Days"`
			} `xml:"DefaultRetention"`
		} `xml:"Rule"`
	}
	if err := xml.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&configuration); err != nil {
		return fmt.Errorf("decode evidence object retention: %w", err)
	}
	if configuration.Enabled != "Enabled" || configuration.Rule.Retention.Mode != "COMPLIANCE" || configuration.Rule.Retention.Days != bucket.retentionDays {
		return errors.New("evidence object storage does not enforce the configured compliance retention")
	}
	return nil
}

func (bucket Bucket) send(ctx context.Context, method, key, query string, body io.Reader, size int64, payloadSHA256 string, headers http.Header) (*http.Response, error) {
	if key != "" && !validKey(key) {
		return nil, errors.New("evidence object key must be a plain relative storage path")
	}
	target := *bucket.endpoint
	target.Path = "/" + bucket.name
	if key != "" {
		target.Path += "/" + key
	}
	target.RawQuery = query
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, fmt.Errorf("build object storage request: %w", err)
	}
	request.ContentLength = size
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-Amz-Content-Sha256", payloadSHA256)
	bucket.sign(request, payloadSHA256, time.Now().UTC())
	return bucket.client.Do(request)
}

func (bucket Bucket) sign(request *http.Request, payloadSHA256 string, now time.Time) {
	timestamp := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	request.Header.Set("X-Amz-Date", timestamp)

	canonicalValues := map[string]string{"host": request.URL.Host}
	for name, values := range request.Header {
		name = strings.ToLower(name)
		if strings.HasPrefix(name, "x-amz-") {
			canonicalValues[name] = strings.TrimSpace(strings.Join(values, ","))
		}
	}
	var names []string
	for name := range canonicalValues {
		names = append(names, name)
	}
	sort.Strings(names)
	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name + ":" + canonicalValues[name] + "\n")
	}
	signedHeaders := strings.Join(names, ";")
	canonicalRequest := strings.Join([]string{
		request.Method,
		request.URL.EscapedPath(),
		request.URL.RawQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadSHA256,
	}, "\n")

	scope := strings.Join([]string{day, bucket.region, storageService, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		signatureAlgorithm,
		timestamp,
		scope,
		hex.EncodeToString(digestOf(canonicalRequest)),
	}, "\n")

	signingKey := sign(sign(sign(sign([]byte("AWS4"+bucket.secretKey), day), bucket.region), storageService), "aws4_request")
	request.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		signatureAlgorithm, bucket.accessKey, scope, signedHeaders, hex.EncodeToString(sign(signingKey, stringToSign))))
}

func sign(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

func digestOf(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

// drain reads the bounded remainder of an answer FrameOPS does not parse so the
// connection can be reused, and never logs the body.
func drain(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
}

func validBucketName(name string) bool {
	if len(name) < 3 || len(name) > 63 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, character := range name {
		if !storageCharacter(character) {
			return false
		}
	}
	return true
}

// validKey guards the object namespace even though every key FrameOPS uses is
// derived by the database from identifiers the caller already owns.
func validKey(key string) bool {
	if strings.HasPrefix(key, "/") || strings.Contains(key, "//") {
		return false
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if !storageCharacter(character) {
				return false
			}
		}
	}
	return true
}

// storageCharacter is the lowercase alphabet shared by bucket names and by the
// identifiers the database concatenates into an object key.
func storageCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-'
}
