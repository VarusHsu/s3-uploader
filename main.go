package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
)

var s3Client *s3.Client
var presignClient *s3.PresignClient

func main() {
	region := envOrDefault("AWS_REGION", "us-west-2")
	addr := envOrDefault("LISTEN_ADDR", ":50001")

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}
	s3Client = s3.NewFromConfig(cfg)
	presignClient = s3.NewPresignClient(s3Client)

	r := gin.Default()
	r.POST("/upload", handleUploadURL)
	r.POST("/upload-url", handleUploadURL)

	log.Printf("listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleUploadURL(c *gin.Context) {
	bucket := c.PostForm("bucket")
	if bucket == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required field: bucket"})
		return
	}

	key := c.PostForm("key")
	if key == "" {
		filename := c.PostForm("filename")
		if filename != "" {
			key = filepath.Base(filename)
		} else {
			key = fmt.Sprintf("uploads/%d", time.Now().UnixNano())
		}
	}

	contentType := c.DefaultPostForm("contentType", "application/octet-stream")
	expiresInSec := int64(120)
	if v := c.PostForm("expiresIn"); v != "" {
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid expiresIn: must be an integer number of seconds"})
			return
		}
		expiresInSec = parsed
	}
	if expiresInSec < 60 || expiresInSec > 3600 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expiresIn must be between 60 and 3600 seconds"})
		return
	}

	presignedReq, err := presignClient.PresignPutObject(c.Request.Context(), &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
		//IfNoneMatch: aws.String("*"),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expiresInSec) * time.Second
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate upload URL: %v", err)})
		return
	}

	headers := map[string]string{}
	for k, vals := range presignedReq.SignedHeader {
		if len(vals) > 0 {
			headers[k] = vals[0]
		}
	}

	expiresAt := time.Now().Add(time.Duration(expiresInSec) * time.Second).UTC().Format(time.RFC3339)
	c.JSON(http.StatusOK, gin.H{
		"message":     "presigned upload URL generated",
		"uploadUrl":   presignedReq.URL,
		"method":      presignedReq.Method,
		"headers":     headers,
		"bucket":      bucket,
		"key":         key,
		"contentType": contentType,
		"singleUse":   true,
		"expiresIn":   expiresInSec,
		"expiresAt":   expiresAt,
		"location":    fmt.Sprintf("s3://%s/%s", bucket, key),
	})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
