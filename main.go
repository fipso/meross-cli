package main

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	merossBaseURL  = "https://iotx.meross.com"
	refossBaseURL  = "https://iotx-eu.refoss.net"
	secretKey      = "23x17ahWarFH6w29"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
	key        string
	userID     string
}

type BaseResponse struct {
	APIStatus int             `json:"apiStatus"`
	SysStatus int             `json:"sysStatus"`
	Info      string          `json:"info"`
	Data      json.RawMessage `json:"data"`
}

type LoginResponse struct {
	Token  string `json:"token"`
	Key    string `json:"key"`
	UserID string `json:"userid"`
	Email  string `json:"email"`
}

type Device struct {
	UUID           string   `json:"uuid"`
	DevName        string   `json:"devName"`
	DevIconID      string   `json:"devIconId"`
	OnlineStatus   int      `json:"onlineStatus"`
	FmwareVersion  string   `json:"fmwareVersion"`
	HdwareVersion  string   `json:"hdwareVersion"`
	DeviceType     string   `json:"deviceType"`
	SubType        string   `json:"subType"`
	Region         string   `json:"region"`
	Domain         string   `json:"domain"`
	ReservedDomain string   `json:"reservedDomain"`
	BindTime       int64    `json:"bindTime"`
	Channels       []Channel `json:"channels"`
}

type Channel struct {
	Channel    int    `json:"channel"`
	DevName    string `json:"devName"`
	Type       string `json:"type"`
	DevIconID  string `json:"devIconId"`
}

// Local device query types
type DeviceRequest struct {
	Header  DeviceRequestHeader `json:"header"`
	Payload struct{}            `json:"payload"`
}

type DeviceRequestHeader struct {
	MessageID      string `json:"messageId"`
	Method         string `json:"method"`
	From           string `json:"from"`
	PayloadVersion int    `json:"payloadVersion"`
	Namespace      string `json:"namespace"`
	UUID           string `json:"uuid"`
	Sign           string `json:"sign"`
	TriggerSrc     string `json:"triggerSrc"`
	Timestamp      int64  `json:"timestamp"`
}

type DeviceResponse struct {
	Header  DeviceRequestHeader `json:"header"`
	Payload struct {
		All struct {
			System struct {
				Hardware struct {
					Type       string `json:"type"`
					SubType    string `json:"subType"`
					Version    string `json:"version"`
					ChipType   string `json:"chipType"`
					UUID       string `json:"uuid"`
					MacAddress string `json:"macAddress"`
				} `json:"hardware"`
				Firmware struct {
					Version      string `json:"version"`
					CompileTime  string `json:"compileTime"`
					WifiMac      string `json:"wifiMac"`
					InnerIP      string `json:"innerIp"`
					Server       string `json:"server"`
					Port         int    `json:"port"`
					UserID       int    `json:"userId"`
				} `json:"firmware"`
			} `json:"system"`
			Digest struct {
				ToggleX []struct {
					Channel int `json:"channel"`
					OnOff   int `json:"onoff"`
				} `json:"togglex"`
			} `json:"digest"`
		} `json:"all"`
	} `json:"payload"`
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = refossBaseURL
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

func randomString(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func (c *Client) signRequest(params map[string]interface{}) url.Values {
	paramsJSON, _ := json.Marshal(params)
	paramsB64 := base64.StdEncoding.EncodeToString(paramsJSON)

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomString(16)

	signData := secretKey + timestamp + nonce + paramsB64
	sign := md5Hash(signData)

	form := url.Values{}
	form.Set("params", paramsB64)
	form.Set("sign", sign)
	form.Set("timestamp", timestamp)
	form.Set("nonce", nonce)

	return form
}

func (c *Client) doRequest(endpoint string, params map[string]interface{}, authenticated bool) (*BaseResponse, error) {
	form := c.signRequest(params)

	req, err := http.NewRequest("POST", c.baseURL+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}

	// Determine vendor based on URL
	vendor := "refoss"
	if strings.Contains(c.baseURL, "meross") {
		vendor = "meross"
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("AppVersion", "1.15.3")
	req.Header.Set("Vendor", vendor)
	req.Header.Set("AppLanguage", "en")
	req.Header.Set("AppType", "iOS")

	if authenticated && c.token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Basic %s", c.token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result BaseResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w\nBody: %s", err, string(body))
	}

	return &result, nil
}

func (c *Client) Login(email, password string) error {
	hashedPassword := md5Hash(password)

	params := map[string]interface{}{
		"email":              email,
		"password":           hashedPassword,
		"encryption":         1,
		"accountCountryCode": "US",
		"mobileInfo": map[string]interface{}{
			"resolution":       "1080*1920",
			"carrier":          "",
			"deviceModel":      "Go,CLI",
			"mobileOs":         "Linux",
			"mobileOsVersion":  "1.0",
			"uuid":             randomString(32),
		},
		"agree": 1,
	}

	resp, err := c.doRequest("/v1/Auth/signIn", params, false)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}

	if resp.APIStatus != 0 {
		return fmt.Errorf("login failed: status=%d, info=%s", resp.APIStatus, resp.Info)
	}

	var loginData LoginResponse
	if err := json.Unmarshal(resp.Data, &loginData); err != nil {
		return fmt.Errorf("failed to parse login data: %w", err)
	}

	c.token = loginData.Token
	c.key = loginData.Key
	c.userID = loginData.UserID

	return nil
}

func (c *Client) GetDevices() ([]Device, error) {
	resp, err := c.doRequest("/v1/Device/devList", map[string]interface{}{}, true)
	if err != nil {
		return nil, fmt.Errorf("device list request failed: %w", err)
	}

	if resp.APIStatus != 0 {
		return nil, fmt.Errorf("device list failed: status=%d, info=%s", resp.APIStatus, resp.Info)
	}

	var devices []Device
	if err := json.Unmarshal(resp.Data, &devices); err != nil {
		return nil, fmt.Errorf("failed to parse device list: %w", err)
	}

	return devices, nil
}

// QueryDevice queries a device directly by IP address
func QueryDevice(ip string, key string, verbose bool) (*DeviceResponse, string, error) {
	messageID := md5Hash(randomString(16) + fmt.Sprintf("%d", time.Now().Unix()))
	timestamp := time.Now().Unix()

	// Sign: MD5(messageId + key + timestamp)
	// For unauthenticated queries, key can be empty
	sign := md5Hash(messageID + key + fmt.Sprintf("%d", timestamp))

	reqData := DeviceRequest{
		Header: DeviceRequestHeader{
			MessageID:      messageID,
			Method:         "GET",
			From:           fmt.Sprintf("http://%s/config", ip),
			PayloadVersion: 1,
			Namespace:      "Appliance.System.All",
			UUID:           "", // Empty for discovery
			Sign:           sign,
			TriggerSrc:     "GoCLI",
			Timestamp:      timestamp,
		},
		Payload: struct{}{},
	}

	reqJSON, err := json.Marshal(reqData)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Request: %s\n", string(reqJSON))
	}

	url := fmt.Sprintf("http://%s/config", ip)
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqJSON))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Response: %s\n", string(body))
	}

	var deviceResp DeviceResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, string(body), fmt.Errorf("failed to parse response: %w\nBody: %s", err, string(body))
	}

	return &deviceResp, string(body), nil
}

func runQuery(args []string) {
	queryFlags := flag.NewFlagSet("query", flag.ExitOnError)
	jsonOutput := queryFlags.Bool("json", false, "Output in JSON format")
	key := queryFlags.String("key", "", "Device key (from cloud login, optional)")
	verbose := queryFlags.Bool("verbose", false, "Show raw response")
	queryFlags.Parse(args)

	if queryFlags.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: meross-cli query <device-ip> [-json] [-key <key>] [-verbose]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Example: meross-cli query 192.168.178.50")
		os.Exit(1)
	}

	ip := queryFlags.Arg(0)
	fmt.Fprintf(os.Stderr, "Querying device at %s...\n", ip)

	resp, rawBody, err := QueryDevice(ip, *key, *verbose)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
		os.Exit(1)
	}

	// Check for error response - device still reveals UUID in header
	if resp.Header.Method == "ERROR" {
		fmt.Println("\nDevice responded with error (key required for full info)")
		fmt.Printf("  UUID: %s\n", resp.Header.UUID)
		fmt.Println("\nTo get full info, use the key from cloud login:")
		fmt.Println("  meross-cli query <ip> -key <key>")
		return
	}

	hw := resp.Payload.All.System.Hardware
	fw := resp.Payload.All.System.Firmware

	if *jsonOutput {
		// Print raw response for full JSON
		fmt.Println(rawBody)
		return
	}

	fmt.Println("\nDevice Info:")
	fmt.Printf("  UUID:       %s\n", hw.UUID)
	fmt.Printf("  Type:       %s\n", hw.Type)
	fmt.Printf("  SubType:    %s\n", hw.SubType)
	fmt.Printf("  MAC:        %s\n", hw.MacAddress)
	fmt.Printf("  IP:         %s\n", fw.InnerIP)
	fmt.Printf("  Firmware:   %s\n", fw.Version)
	fmt.Printf("  Hardware:   %s\n", hw.Version)
	fmt.Printf("  WiFi MAC:   %s\n", fw.WifiMac)
	fmt.Printf("  Server:     %s:%d\n", fw.Server, fw.Port)
	fmt.Printf("  User ID:    %d\n", fw.UserID)

	if len(resp.Payload.All.Digest.ToggleX) > 0 {
		fmt.Println("  Channels:")
		for _, ch := range resp.Payload.All.Digest.ToggleX {
			state := "off"
			if ch.OnOff == 1 {
				state = "on"
			}
			fmt.Printf("    - [%d] %s\n", ch.Channel, state)
		}
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())

	if len(os.Args) > 1 && os.Args[1] == "query" {
		runQuery(os.Args[2:])
		return
	}

	email := flag.String("email", "", "Meross/Refoss account email")
	password := flag.String("password", "", "Meross/Refoss account password")
	baseURL := flag.String("url", "", "API base URL\n    \tRefoss EU: https://iotx-eu.refoss.net (default)\n    \tRefoss US: https://iotx-us.refoss.net\n    \tMeross: https://iotx.meross.com")
	jsonOutput := flag.Bool("json", false, "Output in JSON format")

	flag.Parse()

	if *email == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  meross-cli -email <email> -password <password>   List devices from cloud")
		fmt.Fprintln(os.Stderr, "  meross-cli query <device-ip>                     Query device by IP")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Cloud Options:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Query Options:")
		fmt.Fprintln(os.Stderr, "  -json        Output in JSON format")
		fmt.Fprintln(os.Stderr, "  -key         Device key (from cloud login, optional)")
		os.Exit(1)
	}

	client := NewClient(*baseURL)

	fmt.Fprintf(os.Stderr, "Logging in as %s...\n", *email)
	if err := client.Login(*email, *password); err != nil {
		fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "Login successful!")

	fmt.Fprintln(os.Stderr, "Fetching devices...")
	devices, err := client.GetDevices()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get devices: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		output, _ := json.MarshalIndent(devices, "", "  ")
		fmt.Println(string(output))
		return
	}

	fmt.Printf("\nFound %d device(s):\n\n", len(devices))
	for i, d := range devices {
		status := "offline"
		if d.OnlineStatus == 1 {
			status = "online"
		} else if d.OnlineStatus == 3 {
			status = "upgrading"
		}

		fmt.Printf("%d. %s\n", i+1, d.DevName)
		fmt.Printf("   UUID:     %s\n", d.UUID)
		fmt.Printf("   Type:     %s\n", d.DeviceType)
		fmt.Printf("   Status:   %s\n", status)
		fmt.Printf("   Firmware: %s\n", d.FmwareVersion)
		fmt.Printf("   Hardware: %s\n", d.HdwareVersion)
		fmt.Printf("   Region:   %s\n", d.Region)
		fmt.Printf("   Domain:   %s\n", d.Domain)
		fmt.Printf("   Key:      %s\n", client.key)

		if len(d.Channels) > 1 {
			fmt.Printf("   Channels:\n")
			for _, ch := range d.Channels {
				if ch.DevName != "" {
					fmt.Printf("     - [%d] %s\n", ch.Channel, ch.DevName)
				}
			}
		}
		fmt.Println()
	}
}
