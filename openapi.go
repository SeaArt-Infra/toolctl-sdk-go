package toolctl

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func LoadOpenAPISpec(opts LoadOpenAPIOptions) (map[string]any, error) {
	provided := 0
	if opts.Spec != nil {
		provided++
	}
	if opts.SpecPath != "" {
		provided++
	}
	if opts.SpecURL != "" {
		provided++
	}
	if provided != 1 {
		return nil, &OpenAPIImportError{Message: "provide exactly one of spec, spec_path, or spec_url"}
	}
	if opts.Spec != nil {
		return opts.Spec, nil
	}
	if opts.SpecPath != "" {
		data, err := os.ReadFile(opts.SpecPath)
		if err != nil {
			return nil, &OpenAPIImportError{Message: err.Error()}
		}
		var spec map[string]any
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, &OpenAPIImportError{Message: err.Error()}
		}
		return spec, nil
	}
	client := &http.Client{Timeout: 30 * time.Second}
	verifyTLS := true
	if opts.VerifyTLS != nil {
		verifyTLS = *opts.VerifyTLS
	}
	if !verifyTLS {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, //nolint:gosec
		}
	}
	resp, err := client.Get(opts.SpecURL)
	if err != nil {
		return nil, &OpenAPIImportError{Message: err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &OpenAPIImportError{Message: err.Error()}
	}
	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		return nil, &OpenAPIImportError{Message: err.Error()}
	}
	return spec, nil
}

func FindOpenAPIOperation(opts FindOpenAPIOperationOptions) (string, string, map[string]any, error) {
	paths, ok := opts.Spec["paths"].(map[string]any)
	if !ok {
		return "", "", nil, &OpenAPIImportError{Message: "could not find a matching operation in the OpenAPI spec"}
	}
	normalizedMethod := strings.ToLower(opts.Method)
	for candidatePath, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			continue
		}
		for candidateMethod, rawOperation := range pathItem {
			methodName := strings.ToLower(candidateMethod)
			switch methodName {
			case "get", "post", "put", "patch", "delete":
			default:
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				continue
			}
			if opts.OperationID != "" {
				if value, ok := operation["operationId"].(string); ok && value == opts.OperationID {
					return candidatePath, strings.ToUpper(candidateMethod), operation, nil
				}
			}
			if opts.Path != "" && normalizedMethod != "" && candidatePath == opts.Path && methodName == normalizedMethod {
				return candidatePath, strings.ToUpper(candidateMethod), operation, nil
			}
		}
	}
	return "", "", nil, &OpenAPIImportError{Message: fmt.Sprintf("could not find a matching operation in the OpenAPI spec")}
}
