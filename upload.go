package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type inputError struct {
	msg string
}

func (e inputError) Error() string {
	return e.msg
}

type uploadURLParams struct {
	Bucket      string
	Key         string
	Filename    string
	ContentType string
	ExpiresIn   string
}

type uploadURLResponse struct {
	Message     string            `json:"message"`
	UploadURL   string            `json:"uploadUrl"`
	Method      string            `json:"method"`
	Headers     map[string]string `json:"headers"`
	Bucket      string            `json:"bucket"`
	Key         string            `json:"key"`
	ContentType string            `json:"contentType"`
	SingleUse   bool              `json:"singleUse"`
	ExpiresIn   int64             `json:"expiresIn"`
	ExpiresAt   string            `json:"expiresAt"`
	Location    string            `json:"location"`
}

func generateUploadURL(ctx context.Context, params uploadURLParams) (*uploadURLResponse, error) {
	bucket := strings.TrimSpace(params.Bucket)
	if bucket == "" {
		return nil, inputError{msg: "missing required field: bucket"}
	}

	key := strings.TrimSpace(params.Key)
	if key == "" {
		filename := strings.TrimSpace(params.Filename)
		if filename != "" {
			key = filepath.Base(filename)
		} else {
			key = fmt.Sprintf("uploads/%d", time.Now().UnixNano())
		}
	}

	contentType := strings.TrimSpace(params.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	expiresInSec, err := parseExpiresIn(params.ExpiresIn)
	if err != nil {
		return nil, err
	}

	presignedReq, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expiresInSec) * time.Second
	})
	if err != nil {
		return nil, fmt.Errorf("failed to generate upload URL: %w", err)
	}

	headers := map[string]string{}
	for k, vals := range presignedReq.SignedHeader {
		if strings.EqualFold(k, "host") {
			continue
		}
		if len(vals) > 0 {
			headers[k] = vals[0]
		}
	}

	expiresAt := time.Now().Add(time.Duration(expiresInSec) * time.Second).UTC().Format(time.RFC3339)
	return &uploadURLResponse{
		Message:     "presigned upload URL generated",
		UploadURL:   presignedReq.URL,
		Method:      presignedReq.Method,
		Headers:     headers,
		Bucket:      bucket,
		Key:         key,
		ContentType: contentType,
		SingleUse:   true,
		ExpiresIn:   expiresInSec,
		ExpiresAt:   expiresAt,
		Location:    fmt.Sprintf("s3://%s/%s", bucket, key),
	}, nil
}

func parseExpiresIn(raw string) (int64, error) {
	expiresInSec := int64(120)
	if strings.TrimSpace(raw) != "" {
		parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return 0, inputError{msg: "invalid expiresIn: must be an integer number of seconds"}
		}
		expiresInSec = parsed
	}
	if expiresInSec < 60 || expiresInSec > 3600 {
		return 0, inputError{msg: "expiresIn must be between 60 and 3600 seconds"}
	}
	return expiresInSec, nil
}
