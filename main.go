package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

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

	go func() {
		log.Printf("starting MCP server on %s", mcpListenAddr)
		if err := runMCPServer(context.Background()); err != nil {
			log.Printf("mcp server stopped with error: %v", err)
		}
	}()

	r := gin.Default()
	r.Use(corsMiddleware(parseAllowedOrigins(envOrDefault("CORS_ALLOW_ORIGINS", "*"))))
	r.GET("/", func(c *gin.Context) {
		c.File("./web/index.html")
	})
	r.StaticFile("/app.js", "./web/app.js")
	r.StaticFile("/styles.css", "./web/styles.css")
	r.Static("/web", "./web")
	r.POST("/upload", handleUploadURL)
	r.POST("/upload-url", handleUploadURL)

	log.Printf("listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleUploadURL(c *gin.Context) {
	params := uploadURLParams{
		Bucket:      c.PostForm("bucket"),
		Key:         c.PostForm("key"),
		Filename:    c.PostForm("filename"),
		ContentType: c.PostForm("contentType"),
		ExpiresIn:   c.PostForm("expiresIn"),
	}

	resp, err := generateUploadURL(c.Request.Context(), params)
	if err != nil {
		if _, ok := err.(inputError); ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseAllowedOrigins(raw string) map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		allowed[origin] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed["*"] = struct{}{}
	}
	return allowed
}

func corsMiddleware(allowedOrigins map[string]struct{}) gin.HandlerFunc {
	allowAll := false
	if _, ok := allowedOrigins["*"]; ok {
		allowAll = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if allowAll {
				c.Header("Access-Control-Allow-Origin", "*")
			} else if _, ok := allowedOrigins[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.Header("Access-Control-Max-Age", "600")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
