package utils

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
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
	// Convert https://letraz-all-purpose.blr1.digitaloceanspaces.com to https://blr1.digitaloceanspaces.com
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
