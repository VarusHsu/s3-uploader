package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
)

var s3Client *s3.Client

func main() {
	region := envOrDefault("AWS_REGION", "us-east-1")
	addr := envOrDefault("LISTEN_ADDR", ":50001")

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}
	s3Client = s3.NewFromConfig(cfg)

	r := gin.Default()
	r.POST("/upload", handleUpload)

	log.Printf("listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleUpload(c *gin.Context) {
	bucket := c.PostForm("bucket")
	if bucket == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing required field: bucket"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("missing or invalid file: %v", err)})
		return
	}
	defer file.Close()

	key := c.PostForm("key")
	if key == "" {
		key = filepath.Base(header.Filename)
	}

	_, err = s3Client.PutObject(c.Request.Context(), &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          file,
		ContentLength: aws.Int64(header.Size),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("upload failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":  "uploaded successfully",
		"bucket":   bucket,
		"key":      key,
		"size":     header.Size,
		"location": fmt.Sprintf("s3://%s/%s", bucket, key),
	})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
