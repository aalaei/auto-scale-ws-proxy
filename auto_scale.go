package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"crypto/tls"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"
)

var (
	listenAddr        = getEnv("LISTEN_ADDR", ":8080")
	secretPath        = getEnv("SECRET_PATH", "/vmessws")
	backendPath	   	  = getEnv("BACKEND_PATH", "/ws")
	backendTargetURL  = getEnv("BACKEND_URL", "http://127.0.0.1:3001")
	kubeClusterAPI    = getEnv("KUBE_CLUSTER_ENDPOINT", "https://kubernetes.default.svc")
	kubeNamespace     = getEnv("NAMESPACE", "test")
	deploymentName    = getEnv("DEPLOYMENT_NAME", "t2")
	inactivityMinutes = getEnvAsInt("INACTIVITY_MINUTES", 60)
	ReplicaUpdateIntervalHours = getEnvAsInt("REPLICA_UPDATE_INTERVAL_HOURS", 24) // in hours
	backendHealthCheckInterval = getEnvAsInt("BACKEND_HEALTH_CHECK_INTERVAL", 10) // in minutes

	lastScaledReplicas int = -1 // -1 means unknown/uninitialized

	lastRequestTime time.Time
	lastScaleRequestTime time.Time
	lastBackEndTime time.Time
	mu              sync.Mutex
	httpClient      = &http.Client{Timeout: 5 * time.Second}
)

func main() {
	log.Printf("Smart WebSocket Proxy with Kubernetes auto-scaler starting [%s%s]...\n", listenAddr, secretPath)
	log.Printf("Backend URL: %s on %s path\n", backendTargetURL, backendPath)

	lastRequestTime = time.Now()
	lastScaleRequestTime = time.Time{}
	lastBackEndTime = time.Time{}

	go inactivityWatcher()

	http.HandleFunc(secretPath, handleWebSocketProxy)

	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
func handleWebSocketProxy(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	lastRequestTime = time.Now()
	mu.Unlock()

	if !isBackendUp() {
		log.Println("Backend is down. Scaling up via Kubernetes...")
		if err := scaleDeployment(1); err != nil {
			http.Error(w, "Failed to scale backend up", http.StatusInternalServerError)
			return
		}
		time.Sleep(10 * time.Second)
	}

	target, err := url.Parse(backendTargetURL)
	if err != nil {
		http.Error(w, "Invalid backend URL", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	// Customize the Transport to skip TLS verification
	proxy.Transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}

	// Fix WebSocket upgrade headers
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = backendPath // Change to the backend's actual WebSocket path
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		req.Host = target.Host // Ensure the Host is set to backend's host
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Println("Proxy error:", err)
		http.Error(w, "Proxy error", http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}


func isBackendUp() bool {
	if time.Since(lastBackEndTime) < time.Minute* time.Duration(backendHealthCheckInterval) {
		// log.Println("Using cached backend status")
		return true
	}
	req, err := http.NewRequest("GET", backendTargetURL, nil)
	if err != nil {
		log.Println("Failed to create health check request:", err)
		return false
	}

	// Important: vmess path must match exactly
	req.URL.Path = backendPath

	// Avoid redirects
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Health check failed:", err)
		return false
	}
	defer resp.Body.Close()
	lastBackEndTime = time.Now()

	switch resp.StatusCode {
	case http.StatusBadRequest:
		// 400 Bad Request means it's responding as expected for WebSocket — backend is up
		return true
	case http.StatusNotFound:
		// 404 means backend isn't serving the vmess path yet
		log.Println("Backend is down (404 received)")
		return false
	default:
		log.Printf("Unexpected status code from backend health check: %d\n", resp.StatusCode)
		return false
	}

}


func scaleDeployment(replicas int) error {
	log.Printf("Deployment tried scaled to %d replicas\n", replicas)
	// mu.Lock()
	// if lastScaledReplicas == replicas and it was less than a day since update, we don't need to scale again
	if (lastScaledReplicas == replicas && time.Since(lastScaleRequestTime) < time.Duration(ReplicaUpdateIntervalHours)*time.Hour) {
		log.Printf("Scale unchanged: already at %d replicas\n", replicas)
		// mu.Unlock()
		return nil
	}
	// mu.Unlock()
	log.Printf("Deployment scaled to %d replicas\n", replicas)
	token := os.Getenv("KUBE_CLUSTER_TOKEN")
	if token == "" {
		return fmt.Errorf("KUBE_CLUSTER_TOKEN not set")
	}

	scaleURL := fmt.Sprintf("%s/apis/apps/v1/namespaces/%s/deployments/%s/scale", kubeClusterAPI, kubeNamespace, deploymentName)
	scaleBody := map[string]interface{}{
		"kind":       "Scale",
		"apiVersion": "autoscaling/v1",
		"metadata": map[string]string{
			"name": deploymentName,
		},
		"spec": map[string]int{
			"replicas": replicas,
		},
	}
	bodyBytes, _ := json.Marshal(scaleBody)

	req, err := http.NewRequest(http.MethodPut, scaleURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer " + token)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("K8s API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respData, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("K8s API returned %d: %s", resp.StatusCode, string(respData))
	}

	log.Printf("Deployment scaled to %d replicas\n", replicas)
	lastScaleRequestTime = time.Now()
	lastBackEndTime = time.Time{} // reset backend health check time
	lastScaledReplicas = replicas
	return nil
}

func inactivityWatcher() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		mu.Lock()
		if time.Since(lastRequestTime) >= time.Duration(inactivityMinutes)*time.Minute {
			log.Println("No traffic for a while. Scaling down deployment...")
			if err := scaleDeployment(0); err != nil {
				log.Println("Error scaling down deployment:", err)
				return
			}
		}
		mu.Unlock()
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		i, err := strconv.Atoi(v)
		if err == nil {
			return i
		}
	}
	return fallback
}
