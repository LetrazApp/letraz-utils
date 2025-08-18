package utils

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/uuid"
	"letraz-utils/internal/config"
	"letraz-utils/internal/logging"
	"letraz-utils/internal/logging/types"
)

// SpacesClient wraps the S3 client for DigitalOcean Spaces operations
type SpacesClient struct {
	client     *s3.S3
	bucketName string
	bucketURL  string
	cdnURL     string
	logger     types.Logger
}

// NewSpacesClient creates a new DigitalOcean Spaces client
func NewSpacesClient(cfg *config.Config) (*SpacesClient, error) {
	logger := logging.GetGlobalLogger()

	// Validate configuration
	if cfg.DigitalOcean.Spaces.AccessKeyID == "" || cfg.DigitalOcean.Spaces.AccessKeySecret == "" {
		return nil, fmt.Errorf("DigitalOcean Spaces credentials are required")
	}

	if cfg.DigitalOcean.Spaces.BucketURL == "" {
		return nil, fmt.Errorf("DigitalOcean Spaces bucket URL is required")
	}

	// Extract the region-based endpoint from bucket URL
	endpoint := fmt.Sprintf("https://%s.digitaloceanspaces.com", cfg.DigitalOcean.Spaces.Region)

	logger.Info("Configuring DigitalOcean Spaces with endpoint", map[string]interface{}{
		"endpoint":    endpoint,
		"bucket_name": cfg.DigitalOcean.Spaces.BucketName,
		"region":      cfg.DigitalOcean.Spaces.Region,
	})

	// Configure AWS session for DigitalOcean Spaces
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(
			cfg.DigitalOcean.Spaces.AccessKeyID,
			cfg.DigitalOcean.Spaces.AccessKeySecret,
			"",
		),
		Endpoint:         aws.String(endpoint),
		Region:           aws.String(cfg.DigitalOcean.Spaces.Region),
		S3ForcePathStyle: aws.Bool(false), // Use virtual-hosted-style for DigitalOcean Spaces
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create DigitalOcean Spaces session: %w", err)
	}

	client := s3.New(sess)

	logger.Info("DigitalOcean Spaces client initialized", map[string]interface{}{
		"bucket_name": cfg.DigitalOcean.Spaces.BucketName,
		"region":      cfg.DigitalOcean.Spaces.Region,
		"endpoint":    endpoint,
	})

	return &SpacesClient{
		client:     client,
		bucketName: cfg.DigitalOcean.Spaces.BucketName,
		bucketURL:  cfg.DigitalOcean.Spaces.BucketURL,
		cdnURL:     cfg.DigitalOcean.Spaces.CDNEndpoint,
		logger:     logger,
	}, nil
}

// UploadScreenshot uploads a screenshot to DigitalOcean Spaces
func (sc *SpacesClient) UploadScreenshot(resumeID string, imageData []byte) (string, error) {
	// Define the object key for the screenshot
	objectKey := fmt.Sprintf("resumes/thumbnails/%s.jpg", resumeID)

	sc.logger.Info("Uploading screenshot to DigitalOcean Spaces", map[string]interface{}{
		"resume_id":  resumeID,
		"object_key": objectKey,
		"size_bytes": len(imageData),
	})

	// Delete any existing screenshot for this resume
	if err := sc.deleteExistingScreenshot(resumeID); err != nil {
		sc.logger.Warn("Failed to delete existing screenshot, continuing with upload", map[string]interface{}{
			"resume_id": resumeID,
			"error":     err.Error(),
		})
	}

	// Upload the new screenshot
	_, err := sc.client.PutObject(&s3.PutObjectInput{
		Bucket:      aws.String(sc.bucketName),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(imageData),
		ContentType: aws.String("image/jpeg"),
		ACL:         aws.String("public-read"), // Make the file publicly accessible
	})

	if err != nil {
		sc.logger.Error("Failed to upload screenshot to DigitalOcean Spaces", map[string]interface{}{
			"resume_id":  resumeID,
			"object_key": objectKey,
			"error":      err.Error(),
		})
		return "", fmt.Errorf("failed to upload screenshot: %w", err)
	}

	// Construct the CDN URL
	var screenshotURL string
	if sc.cdnURL != "" {
		screenshotURL = fmt.Sprintf("%s/%s", strings.TrimRight(sc.cdnURL, "/"), objectKey)
	} else {
		// Fallback to direct bucket URL if CDN is not configured
		// Use the stored bucket URL directly
		if sc.bucketURL != "" {
			bucketBaseURL := strings.TrimRight(sc.bucketURL, "/")
			if !strings.HasPrefix(bucketBaseURL, "https://") {
				bucketBaseURL = "https://" + bucketBaseURL
			}
			screenshotURL = fmt.Sprintf("%s/%s", bucketBaseURL, objectKey)
		} else {
			// Last resort: construct from region and bucket name
			region := ""
			if sc.client.Config.Region != nil {
				region = *sc.client.Config.Region
			}
			screenshotURL = fmt.Sprintf("https://%s.%s.digitaloceanspaces.com/%s",
				sc.bucketName,
				region,
				objectKey,
			)
		}
	}

	sc.logger.Info("Screenshot uploaded successfully", map[string]interface{}{
		"resume_id":      resumeID,
		"object_key":     objectKey,
		"screenshot_url": screenshotURL,
	})

	return screenshotURL, nil
}

// deleteExistingScreenshot removes any existing screenshot for the given resume ID
func (sc *SpacesClient) deleteExistingScreenshot(resumeID string) error {
	// List all objects with the prefix for this resume
	prefix := fmt.Sprintf("resumes/thumbnails/%s.", resumeID)

	listResult, err := sc.client.ListObjectsV2(&s3.ListObjectsV2Input{
		Bucket: aws.String(sc.bucketName),
		Prefix: aws.String(prefix),
	})

	if err != nil {
		return fmt.Errorf("failed to list existing screenshots: %w", err)
	}

	// Delete each found object
	for _, obj := range listResult.Contents {
		_, err := sc.client.DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(sc.bucketName),
			Key:    obj.Key,
		})

		if err != nil {
			sc.logger.Warn("Failed to delete existing screenshot object", map[string]interface{}{
				"resume_id":  resumeID,
				"object_key": *obj.Key,
				"error":      err.Error(),
			})
		} else {
			sc.logger.Info("Deleted existing screenshot", map[string]interface{}{
				"resume_id":  resumeID,
				"object_key": *obj.Key,
			})
		}
	}

	return nil
}

// IsHealthy checks if the Spaces client can communicate with the service
func (sc *SpacesClient) IsHealthy() bool {
	_, err := sc.client.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(sc.bucketName),
	})

	healthy := err == nil
	if !healthy {
		sc.logger.Error("DigitalOcean Spaces health check failed", map[string]interface{}{
			"bucket_name": sc.bucketName,
			"error":       err.Error(),
		})
	}

	return healthy
}

// uploadExport centralizes the logic for uploading export artifacts to Spaces
func (sc *SpacesClient) uploadExport(resumeID string, fileName string, data []byte, contentType string, ext string) (string, error) {
	if resumeID == "" {
		return "", fmt.Errorf("resumeID is required")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("data is empty")
	}
	if ext == "" {
		return "", fmt.Errorf("ext is required")
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	if fileName == "" {
		fileName = uuid.New().String() + ext
	} else {
		// Normalize and constrain to a safe base name
		fileName = filepath.Base(strings.TrimSpace(fileName))
		if fileName == "." || fileName == "" {
			fileName = uuid.New().String() + ext
		}
		if !strings.HasSuffix(strings.ToLower(fileName), strings.ToLower(ext)) {
			fileName += ext
		}
	}

	objectKey := fmt.Sprintf("exports/%s/%s", resumeID, fileName)

	sc.logger.Info("Uploading export to DigitalOcean Spaces", map[string]interface{}{
		"resume_id":    resumeID,
		"object_key":   objectKey,
		"size_bytes":   len(data),
		"content_type": contentType,
	})

	_, err := sc.client.PutObject(&s3.PutObjectInput{
		Bucket:      aws.String(sc.bucketName),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
		ACL:         aws.String("public-read"),
	})
	if err != nil {
		sc.logger.Error("Failed to upload export to DigitalOcean Spaces", map[string]interface{}{
			"resume_id":    resumeID,
			"object_key":   objectKey,
			"error":        err.Error(),
			"content_type": contentType,
		})
		return "", fmt.Errorf("failed to upload export: %w", err)
	}

	// Construct the URL (prefer CDN, then bucket URL, then region-based URL)
	var fileURL string
	if sc.cdnURL != "" {
		fileURL = fmt.Sprintf("%s/%s", strings.TrimRight(sc.cdnURL, "/"), objectKey)
	} else if sc.bucketURL != "" {
		bucketBaseURL := strings.TrimRight(sc.bucketURL, "/")
		if !strings.HasPrefix(bucketBaseURL, "https://") {
			bucketBaseURL = "https://" + bucketBaseURL
		}
		fileURL = fmt.Sprintf("%s/%s", bucketBaseURL, objectKey)
	} else {
		region := ""
		if sc.client.Config.Region != nil {
			region = *sc.client.Config.Region
		}
		fileURL = fmt.Sprintf("https://%s.%s.digitaloceanspaces.com/%s", sc.bucketName, region, objectKey)
	}

	sc.logger.Info("Export uploaded successfully", map[string]interface{}{
		"resume_id":  resumeID,
		"object_key": objectKey,
		"url":        fileURL,
	})

	return fileURL, nil
}

// UploadLatexExport uploads a LaTeX export to DigitalOcean Spaces under exports/<resumeId>/<random>.tex
func (sc *SpacesClient) UploadLatexExport(resumeID string, fileName string, latexData []byte) (string, error) {
	return sc.uploadExport(resumeID, fileName, latexData, "application/x-tex", ".tex")
}

// UploadPDFExport uploads a compiled PDF export to DigitalOcean Spaces under exports/<resumeId>/<fileName>.pdf
func (sc *SpacesClient) UploadPDFExport(resumeID string, fileName string, pdfData []byte) (string, error) {
	return sc.uploadExport(resumeID, fileName, pdfData, "application/pdf", ".pdf")
}

// DeleteExportObject deletes an export artifact under exports/<resumeId>/<fileName>.
// This is a best-effort helper that can be used to clean up partial exports.
func (sc *SpacesClient) DeleteExportObject(resumeID string, fileName string) error {
	if resumeID == "" {
		return fmt.Errorf("resumeID is required")
	}
	if strings.TrimSpace(fileName) == "" {
		return fmt.Errorf("fileName is required")
	}

	safeFileName := filepath.Base(strings.TrimSpace(fileName))
	objectKey := fmt.Sprintf("exports/%s/%s", resumeID, safeFileName)

	sc.logger.Info("Deleting export object from DigitalOcean Spaces", map[string]interface{}{
		"resume_id":  resumeID,
		"object_key": objectKey,
	})

	_, err := sc.client.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(sc.bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		sc.logger.Warn("Failed to delete export object from DigitalOcean Spaces", map[string]interface{}{
			"resume_id":  resumeID,
			"object_key": objectKey,
			"error":      err.Error(),
		})
		return fmt.Errorf("failed to delete export object: %w", err)
	}

	sc.logger.Info("Export object deleted successfully", map[string]interface{}{
		"resume_id":  resumeID,
		"object_key": objectKey,
	})

	return nil
}
