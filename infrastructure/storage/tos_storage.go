package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/drama-generator/backend/pkg/config"
	"github.com/drama-generator/backend/pkg/logger"
	"github.com/google/uuid"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

type TOSStorage struct {
	client    *tos.ClientV2
	bucket    string
	publicURL string
	log       *logger.Logger
}

func NewTOSStorage(cfg *config.TOSConfig, log *logger.Logger) (*TOSStorage, error) {
	if cfg == nil || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return &TOSStorage{log: log}, nil
	}

	client, err := tos.NewClientV2(cfg.Endpoint, tos.WithRegion(cfg.Region),
		tos.WithCredentials(tos.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create TOS client: %w", err)
	}

	publicURL := cfg.PublicURL
	if publicURL == "" {
		publicURL = fmt.Sprintf("https://%s.%s", cfg.Bucket, cfg.Endpoint)
	}
	publicURL = strings.TrimRight(publicURL, "/")

	return &TOSStorage{
		client:    client,
		bucket:    cfg.Bucket,
		publicURL: publicURL,
		log:       log,
	}, nil
}

func (s *TOSStorage) IsConfigured() bool {
	return s.client != nil && s.bucket != ""
}

func (s *TOSStorage) UploadFromPath(localPath, category string) (string, error) {
	if !s.IsConfigured() {
		return "", fmt.Errorf("TOS is not configured")
	}

	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", fmt.Errorf("failed to read local file %s: %w", localPath, err)
	}

	key := s.generateKey(category, filepath.Ext(localPath))
	contentType := detectContentType(filepath.Ext(localPath))

	input := &tos.PutObjectV2Input{}
	input.Bucket = s.bucket
	input.Key = key
	input.Content = bytes.NewReader(data)
	input.ContentLength = int64(len(data))
	input.ContentType = contentType

	if _, err = s.client.PutObjectV2(context.Background(), input); err != nil {
		return "", fmt.Errorf("failed to upload to TOS: %w", err)
	}

	url := fmt.Sprintf("%s/%s", s.publicURL, key)
	s.log.Infow("Uploaded local file to TOS", "local_path", localPath, "tos_url", url)
	return url, nil
}

func (s *TOSStorage) UploadFromURL(url, category string) (string, error) {
	if !s.IsConfigured() {
		return "", fmt.Errorf("TOS is not configured")
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download from URL %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download from URL %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	ext := getFileExtension(url, resp.Header.Get("Content-Type"))
	key := s.generateKey(category, ext)
	contentType := detectContentType(ext)

	input := &tos.PutObjectV2Input{}
	input.Bucket = s.bucket
	input.Key = key
	input.Content = bytes.NewReader(data)
	input.ContentLength = int64(len(data))
	input.ContentType = contentType

	if _, err = s.client.PutObjectV2(context.Background(), input); err != nil {
		return "", fmt.Errorf("failed to upload to TOS: %w", err)
	}

	tosURL := fmt.Sprintf("%s/%s", s.publicURL, key)
	s.log.Infow("Uploaded remote file to TOS", "source_url", url, "tos_url", tosURL)
	return tosURL, nil
}

func (s *TOSStorage) generateKey(category, ext string) string {
	date := time.Now().Format("2006-01-02")
	uid := uuid.New().String()[:8]
	return fmt.Sprintf("%s/%s/%s%s", category, date, uid, ext)
}

func detectContentType(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".aac":
		return "audio/aac"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}
