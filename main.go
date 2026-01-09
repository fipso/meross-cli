package main

import (
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
	merossBaseURL  = "https://iot.meross.com"
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

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("AppVersion", "1.15.3")
	req.Header.Set("Vendor", "refoss")
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

func main() {
	rand.Seed(time.Now().UnixNano())

	email := flag.String("email", "", "Meross/Refoss account email")
	password := flag.String("password", "", "Meross/Refoss account password")
	baseURL := flag.String("url", "", "API base URL\n    \tRefoss EU: https://iotx-eu.refoss.net (default)\n    \tRefoss US: https://iotx-us.refoss.net\n    \tMeross: https://iot.meross.com")
	jsonOutput := flag.Bool("json", false, "Output in JSON format")

	flag.Parse()

	if *email == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "Usage: meross-cli -email <email> -password <password>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
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
