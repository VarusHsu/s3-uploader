package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
)

const mcpListenAddr = ":50002"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorObject `json:"error,omitempty"`
}

type rpcErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func runMCPServer(ctx context.Context) error {
	ln, err := net.Listen("tcp", mcpListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", mcpListenAddr, err)
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		go func(c net.Conn) {
			defer c.Close()
			if err := serveMCPConn(ctx, c); err != nil && err != io.EOF {
				fmt.Printf("mcp client error: %v\n", err)
			}
		}(conn)
	}
}

func serveMCPConn(ctx context.Context, conn net.Conn) error {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		payload, err := readMCPMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req rpcRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			if err := writeResponse(writer, rpcResponse{
				JSONRPC: "2.0",
				Error: &rpcErrorObject{
					Code:    -32700,
					Message: "parse error",
				},
			}); err != nil {
				return err
			}
			continue
		}

		resp, shouldRespond := handleMCPRequest(ctx, req)
		if !shouldRespond {
			continue
		}
		if err := writeResponse(writer, resp); err != nil {
			return err
		}
	}
}

func handleMCPRequest(ctx context.Context, req rpcRequest) (rpcResponse, bool) {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]any{
				"name":    "s3-uploader",
				"version": "1.0.0",
			},
		}
		return resp, true
	case "notifications/initialized":
		return rpcResponse{}, false
	case "ping":
		resp.Result = map[string]any{}
		return resp, true
	case "tools/list":
		resp.Result = map[string]any{
			"tools": []map[string]any{buildGenerateUploadURLTool()},
		}
		return resp, true
	case "tools/call":
		result, err := callTool(ctx, req.Params)
		if err != nil {
			resp.Error = &rpcErrorObject{
				Code:    -32000,
				Message: err.Error(),
			}
			return resp, true
		}
		resp.Result = result
		return resp, true
	default:
		resp.Error = &rpcErrorObject{Code: -32601, Message: "method not found"}
		return resp, true
	}
}

func buildGenerateUploadURLTool() map[string]any {
	return map[string]any{
		"name":        "generate_upload_url",
		"description": "Generate a presigned S3 PUT URL",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"bucket": map[string]any{
					"type":        "string",
					"description": "S3 bucket name",
				},
				"key": map[string]any{
					"type":        "string",
					"description": "S3 object key",
				},
				"filename": map[string]any{
					"type":        "string",
					"description": "Fallback filename when key is empty",
				},
				"contentType": map[string]any{
					"type":        "string",
					"description": "MIME type for upload",
				},
				"expiresIn": map[string]any{
					"description": "Expiration in seconds (60-3600)",
					"oneOf": []map[string]any{
						{"type": "string"},
						{"type": "integer"},
					},
				},
			},
			"required": []string{"bucket"},
		},
	}
}

func callTool(ctx context.Context, rawParams json.RawMessage) (map[string]any, error) {
	var callReq struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(rawParams, &callReq); err != nil {
		return nil, fmt.Errorf("invalid tools/call params: %w", err)
	}

	if callReq.Name != "generate_upload_url" {
		return nil, fmt.Errorf("unknown tool: %s", callReq.Name)
	}

	params, err := uploadParamsFromMCPArgs(callReq.Arguments)
	if err != nil {
		return nil, err
	}

	resp, err := generateUploadURL(ctx, params)
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"message":     resp.Message,
		"uploadUrl":   resp.UploadURL,
		"method":      resp.Method,
		"headers":     resp.Headers,
		"bucket":      resp.Bucket,
		"key":         resp.Key,
		"contentType": resp.ContentType,
		"singleUse":   resp.SingleUse,
		"expiresIn":   resp.ExpiresIn,
		"expiresAt":   resp.ExpiresAt,
		"location":    resp.Location,
	}

	textData, _ := json.Marshal(structured)
	return map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": string(textData),
			},
		},
		"structuredContent": structured,
	}, nil
}

func uploadParamsFromMCPArgs(args map[string]interface{}) (uploadURLParams, error) {
	params := uploadURLParams{
		Bucket:      asString(args["bucket"]),
		Key:         asString(args["key"]),
		Filename:    asString(args["filename"]),
		ContentType: asString(args["contentType"]),
	}

	if rawExpires, ok := args["expiresIn"]; ok {
		switch v := rawExpires.(type) {
		case string:
			params.ExpiresIn = v
		case float64:
			params.ExpiresIn = strconv.FormatInt(int64(v), 10)
		default:
			return uploadURLParams{}, inputError{msg: "invalid expiresIn: must be a string or integer"}
		}
	}

	return params, nil
}

func asString(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func readMCPMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "Content-Length") {
			v, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", err)
			}
			contentLength = v
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeResponse(w *bufio.Writer, resp rpcResponse) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = w.WriteString(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload)))
	if err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}
