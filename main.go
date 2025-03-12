// Package main implements a reverse proxy server for the Letta API.
// A reverse proxy acts as an intermediary server that forwards requests
// from clients to a Letta API server.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
)

// Default API server URL. It will be overridden by CLI flag o r env var.
var targetURL = "http://localhost:8283/v1"

// Proxy configuration.
type Config struct {
	APIServer string `json:"api_server"`
}

// getConfigPath returns the path to the configuration file.
func getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Error getting home directory: %v", err)
		return ""
	}
	return filepath.Join(home, "letta-api-server.json")
}

// loadConfig loads the configuration from file.
func loadConfig() *Config {
	configPath := getConfigPath()
	if configPath == "" {
		return nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading config file: %v", err)
		}
		return nil
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("Error parsing config file: %v", err)
		return nil
	}

	return &config
}

// saveConfig saves the configuration to the user's home directory /home/user/letta-api-server.json, /User/user/letta-api-server.json, C:\Users\user\letta-api-server.json.
func saveConfig(apiServer string) {
	configPath := getConfigPath()
	if configPath == "" {
		return
	}

	config := Config{APIServer: apiServer}
	data, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		log.Printf("Error marshaling config: %v", err)
		return
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		log.Printf("Error writing config file: %v", err)
	}
}

// getTargetURL determines the API server URL using the following priority:
// 1. Environment variable LETTA_API_SERVER.
// 2. Command line flag -api-server.
// 3. Saved configuration file.
// 4. Default value.
func getTargetURL() (string, string, int) {

	// Parse command line flags
	var apiServer string
	var host string
	var port int
	flag.StringVar(&apiServer, "api-server", "", "Letta API server URL (example: http://localhost:8283/v1)")
	flag.StringVar(&host, "host", "0.0.0.0", "Proxy host to listen on.")
	flag.IntVar(&port, "port", 8284, "Proxy port to listen on.")

	flag.Parse()

	// Default host is 0.0.0.0.
	if host == "" {

		host = "0.0.0.0"
	}

	// Default port is 8284.
	if port == 0 {

		port = 8284
	}

	// Check environment variable first.
	if envURL := os.Getenv("LETTA_API_SERVER"); envURL != "" {
		return strings.TrimSuffix(envURL, "/"), host, port
	}

	// Check command line flag.
	if apiServer != "" {
		apiServer = strings.TrimSuffix(apiServer, "/")
		saveConfig(apiServer) // Save for future use
		return apiServer, host, port
	}

	// Try to load from config file
	if config := loadConfig(); config != nil && config.APIServer != "" {
		return config.APIServer, host, port
	}

	// Fall back to default
	return targetURL, host, port
}

// setCORSHeaders configures Cross-Origin Resource Sharing (CORS) headers.
// CORS is a security feature that lets browsers know if they're allowed to
// make requests to our API from different domains/origins.
func setCORSHeaders(w http.ResponseWriter) {
	// Allow requests from any website/domain.
	// In production, you might want to restrict this to specific domains.
	origin := "*"
	w.Header().Set("Access-Control-Allow-Origin", origin)

	// Tell browsers which HTTP methods are allowed.
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")

	// Tell browsers which headers they can include in requests.
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, Origin, User-Agent, Cache-Control, X-Requested-With")

	// Cache CORS preflight requests for 24 hours (86400 seconds).
	// This reduces the number of OPTIONS requests browsers need to make.
	w.Header().Set("Access-Control-Max-Age", "86400")

	// Allow browsers to read custom headers in responses.
	w.Header().Set("Access-Control-Expose-Headers", "*")

	// Help caching work correctly with CORS.
	// The Vary header tells caches to store separate versions based on these headers.
	w.Header().Add("Vary", "Origin")
	w.Header().Add("Vary", "Access-Control-Request-Method")
	w.Header().Add("Vary", "Access-Control-Request-Headers")
}

func handleOptions(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	// Handle preflight request.
	w.WriteHeader(http.StatusOK)
}

// proxyRequest is the main function that handles forwarding requests to the target API.
// 1. Read the incoming request.
// 2. Create a new request to our target.
// 3. Copy over relevant headers and body.
// 4. Send the request and get response.
// 5. Send the response back to the original client.
// https://developer.mozilla.org/en-US/docs/Web/HTTP/CORS
func proxyRequest(w http.ResponseWriter, r *http.Request) {

	// Set CORS headers first - security headers should be set early.
	setCORSHeaders(w)

	// Handle preflight CORS requests.
	// Browsers send OPTIONS requests first to check if CORS is allowed.
	if r.Method == "OPTIONS" {
		handleOptions(w, r)
		return
	}

	// Normalize the URL path to handle trailing slashes consistently.
	path := strings.TrimPrefix(r.URL.Path, "/")
	if strings.HasSuffix(r.URL.Path, "/") && !strings.HasSuffix(path, "/") {
		path += "/"
	}
	// Construct the full URL we'll forward to.
	url := targetURL + "/" + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}

	// Log details for debugging.
	log.Printf("Normalized URL: %s", url)
	log.Printf("Original request Content-Type: %s", r.Header.Get("Content-Type"))
	log.Printf("Original request Content-Length: %s", r.Header.Get("Content-Length"))

	// Read and store the body - we need to do this because:
	// 1. The body can only be read once.
	// 2. We might need to modify it.
	// 3. We need to know its size.
	var bodyData []byte
	var err error
	if r.Body != nil {
		bodyData, err = io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("Read request body (%d bytes): %s", len(bodyData), string(bodyData))
		r.Body.Close()
	}

	// Create a new request to our target API.
	// This is a fresh request that will contain the original request's data.
	req, err := http.NewRequest(r.Method, url, bytes.NewBuffer(bodyData))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers but skip "hop-by-hop" headers.
	// Hop-by-hop headers are meant for a single transport link, not the whole chain.
	for name, values := range r.Header {
		if !isHopByHopHeader(name) {
			for _, value := range values {
				req.Header.Set(name, value)
			}
		}
	}

	// Ensure proper content length and type for POST requests.
	if len(bodyData) > 0 {
		req.ContentLength = int64(len(bodyData))
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	// Create HTTP client that doesn't follow redirects.
	// We want to pass redirects back to the original client.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Actually send the request to our target API.
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error forwarding request: %v", err)
		// Headers already set at start of function.
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers but keep our CORS headers.
	// We don't want the target API's CORS headers (if any).
	for name, values := range resp.Header {
		if !isHopByHopHeader(name) && !strings.HasPrefix(strings.ToLower(name), "access-control-") {
			for _, value := range values {
				w.Header().Set(name, value)
			}
		}
	}
	// Ensure our CORS headers are present.
	setCORSHeaders(w)

	// Log response status.
	log.Printf("Response status: %d", resp.StatusCode)

	// Send the response status and body back to the original client.
	w.WriteHeader(resp.StatusCode)
	written, err := io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Error copying response body after %d bytes: %v", written, err)
	} else {
		log.Printf("Successfully proxied response: status=%d, bytes=%d", resp.StatusCode, written)
	}
}

// Helper function to check hop-by-hop headers.
func isHopByHopHeader(header string) bool {
	hopByHop := map[string]bool{
		"connection":          true,
		"keep-alive":          true,
		"proxy-authenticate":  true,
		"proxy-authorization": true,
		"te":                  true,
		"trailers":            true,
		"transfer-encoding":   true,
		"upgrade":             true,
		"content-length":      true,
	}
	return hopByHop[strings.ToLower(header)]
}

// handleFileUpload specifically handles file upload requests with multipart form data.
// It processes the file upload, forwards it to the target API, and returns the response.
//
// Parameters:
//   - w: Response writer to send the proxied response.
//   - r: The original HTTP request containing the file upload.
//
// The function:
//   - Limits file size to 10MB.
//   - Preserves the original filename.
//   - Maintains content-type headers.
//   - Handles streaming of file data.
func handleFileUpload(w http.ResponseWriter, r *http.Request) {

	// Set CORS headers first.
	setCORSHeaders(w)

	// Handle preflight requests at the beginning.
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	url := targetURL + r.URL.Path

	client := &http.Client{}
	req, err := http.NewRequest(r.Method, url, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers from the original request to the new request.
	for name, values := range r.Header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	// Handle file upload for POST requests.
	if strings.Contains(r.Method, "POST") {
		err = r.ParseMultipartForm(10 << 20)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		file, handler, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		part, err := writer.CreateFormFile("file", handler.Filename)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, err = io.Copy(part, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = writer.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		req.Body = io.NopCloser(body)
		req.ContentLength = int64(body.Len())
	}

	// Forward the request to the target API.
	resp, err := client.Do(req)
	if err != nil {
		setCORSHeaders(w)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy all headers first.
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Set(name, value)
		}
	}

	// Then write status code.
	w.WriteHeader(resp.StatusCode)

	// Finally, copy the body.
	if _, err = io.Copy(w, resp.Body); err != nil {
		log.Printf("Error copying response body: %v", err)
		return
	}
}

// main initializes and starts the proxy server.
// It sets up request routing and logging, and starts listening on port 8284.
//
// The server provides:
//   - Logging of all requests.
//   - Automatic CORS handling.
//   - Special handling for file uploads.
//   - Generic request proxying.
//
// The server will exit with log.Fatal if it fails to start.
func main() {
	// Get API server URL from available sources.
	targetURL, host, port := getTargetURL()
	log.Printf("Letta API server set to: %s", targetURL)

	r := mux.NewRouter()

	// Main request handler for all paths.
	r.HandleFunc("/{path:.*}", func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers immediately.
		setCORSHeaders(w)

		// Handle OPTIONS requests first.
		if r.Method == "OPTIONS" {
			handleOptions(w, r)
			return
		}

		// Request logging.
		log.Printf("Request: %s %s", r.Method, r.URL.Path)
		log.Printf("Headers: %v", r.Header)

		// Body handling - ensures the body can be read multiple times if needed.
		if r.Body != nil {
			if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
				bodyBytes, _ := io.ReadAll(r.Body)
				if len(bodyBytes) > 0 {
					log.Printf("Body: %s", string(bodyBytes))
				}
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			} else {
				log.Printf("Multipart form data detected")
			}
		}

		// Route request based on content type and method.
		if strings.Contains(r.Method, "POST") && r.MultipartForm != nil {
			log.Printf("Handling file upload")
			handleFileUpload(w, r)
		} else {
			log.Printf("Proxying standard request")
			proxyRequest(w, r)
		}
	})

	log.Printf("#################################################")
	if host == "0.0.0.0" {

		log.Printf("Point your Letta client to an available IP on this proxy host using port %d.", port)

	} else {

		log.Printf("Point your Letta client to the proxy host http://%s:%d", host, port)

	}

	listenOn := fmt.Sprintf("%s:%d", host, port)
	err := http.ListenAndServe(listenOn, r)
	if err != nil {
		log.Fatal(err)
	}

}
