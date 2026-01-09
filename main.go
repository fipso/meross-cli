package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
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

// UdpDevice represents a device discovered via UDP broadcast
type UdpDevice struct {
	UUID        string `json:"uuid"`
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	DeviceType  string `json:"deviceType"`
	DevName     string `json:"devName"`
	Port        int    `json:"port"`
	DevHardWare string `json:"devHardWare"`
	DevSoftWare string `json:"devSoftWare"`
	SubType     string `json:"subType"`
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

// DiscoverDevices finds devices on the local network via UDP broadcast
func DiscoverDevices(timeout time.Duration, verbose bool, broadcastIP string) ([]UdpDevice, error) {
	// Listen on port 9989 for responses
	listenAddr, err := net.ResolveUDPAddr("udp", ":9989")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve listen address: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Binding to UDP port 9989...\n")
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on UDP port 9989: %w", err)
	}
	defer conn.Close()

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Successfully bound to port 9989\n")
	}

	// Enable broadcast
	broadcastAddr, err := net.ResolveUDPAddr("udp", broadcastIP+":9988")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve broadcast address: %w", err)
	}

	// Try multiple discovery request formats
	// 1. Wildcard for all devices
	// 2. Special ID used by app for unbound device discovery
	requests := []string{
		`{"id":"*","devName":"*"}`,
		`{"id":"4305487a249f71fa2a0296c1d2655b56","devName":"*"}`,
	}

	for _, request := range requests {
		if verbose {
			fmt.Fprintf(os.Stderr, "[DEBUG] Sending broadcast to %s:9988: %s\n", broadcastIP, request)
		}

		_, err = conn.WriteToUDP([]byte(request), broadcastAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to send discovery broadcast: %w", err)
		}
	}

	return collectResponses(conn, timeout, verbose)
}

// DiscoverDeviceByUUID finds a specific device by UUID on the local network
func DiscoverDeviceByUUID(uuid string, timeout time.Duration, verbose bool, broadcastIP string) ([]UdpDevice, error) {
	listenAddr, err := net.ResolveUDPAddr("udp", ":9989")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve listen address: %w", err)
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Binding to UDP port 9989...\n")
	}

	conn, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on UDP port 9989: %w", err)
	}
	defer conn.Close()

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Successfully bound to port 9989\n")
	}

	broadcastAddr, err := net.ResolveUDPAddr("udp", broadcastIP+":9988")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve broadcast address: %w", err)
	}

	request := fmt.Sprintf(`{"id":"%s","devName":"*"}`, uuid)
	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Sending broadcast to %s:9988: %s\n", broadcastIP, request)
	}

	_, err = conn.WriteToUDP([]byte(request), broadcastAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to send discovery broadcast: %w", err)
	}

	return collectResponses(conn, timeout, verbose)
}

func collectResponses(conn *net.UDPConn, timeout time.Duration, verbose bool) ([]UdpDevice, error) {

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Broadcast sent, waiting for responses (timeout: %v)...\n", timeout)
	}

	// Collect responses
	devices := make(map[string]UdpDevice) // Use map to dedupe by UUID
	conn.SetReadDeadline(time.Now().Add(timeout))

	buf := make([]byte, 4096)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			// Timeout is expected - means we're done collecting responses
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if verbose {
					fmt.Fprintf(os.Stderr, "[DEBUG] Timeout reached, stopping discovery\n")
				}
				break
			}
			return nil, fmt.Errorf("error reading UDP response: %w", err)
		}

		if verbose {
			fmt.Fprintf(os.Stderr, "[DEBUG] Received %d bytes from %s: %s\n", n, addr.String(), string(buf[:n]))
		}

		var device UdpDevice
		if err := json.Unmarshal(buf[:n], &device); err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "[DEBUG] Failed to parse response: %v\n", err)
			}
			continue
		}

		// Fill in IP from response address if not in payload
		if device.IP == "" {
			device.IP = addr.IP.String()
		}

		if device.UUID != "" {
			if verbose {
				fmt.Fprintf(os.Stderr, "[DEBUG] Found device: UUID=%s, IP=%s, MAC=%s\n", device.UUID, device.IP, device.MAC)
			}
			devices[device.UUID] = device
		}
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "[DEBUG] Discovery complete, found %d device(s)\n", len(devices))
	}

	// Convert map to slice
	result := make([]UdpDevice, 0, len(devices))
	for _, d := range devices {
		result = append(result, d)
	}

	return result, nil
}

func runDiscover(args []string) {
	discoverFlags := flag.NewFlagSet("discover", flag.ExitOnError)
	jsonOutput := discoverFlags.Bool("json", false, "Output in JSON format")
	timeout := discoverFlags.Int("timeout", 3, "Discovery timeout in seconds")
	verbose := discoverFlags.Bool("verbose", false, "Show debug output")
	uuid := discoverFlags.String("uuid", "", "Discover specific device by UUID")
	broadcast := discoverFlags.String("broadcast", "255.255.255.255", "Broadcast IP address (try your subnet broadcast, e.g. 192.168.1.255)")
	discoverFlags.Parse(args)

	fmt.Fprintln(os.Stderr, "Discovering devices on local network...")

	var devices []UdpDevice
	var err error

	if *uuid != "" {
		devices, err = DiscoverDeviceByUUID(*uuid, time.Duration(*timeout)*time.Second, *verbose, *broadcast)
	} else {
		devices, err = DiscoverDevices(time.Duration(*timeout)*time.Second, *verbose, *broadcast)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Discovery failed: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput {
		output, _ := json.MarshalIndent(devices, "", "  ")
		fmt.Println(string(output))
		return
	}

	if len(devices) == 0 {
		fmt.Println("\nNo devices found on local network.")
		return
	}

	fmt.Printf("\nFound %d device(s):\n\n", len(devices))
	for i, d := range devices {
		fmt.Printf("%d. %s\n", i+1, d.DevName)
		fmt.Printf("   UUID:     %s\n", d.UUID)
		fmt.Printf("   IP:       %s\n", d.IP)
		fmt.Printf("   MAC:      %s\n", d.MAC)
		fmt.Printf("   Type:     %s\n", d.DeviceType)
		fmt.Printf("   Firmware: %s\n", d.DevSoftWare)
		fmt.Printf("   Hardware: %s\n", d.DevHardWare)
		if d.Port != 0 {
			fmt.Printf("   Port:     %d\n", d.Port)
		}
		fmt.Println()
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  meross-cli -email <email> -password <password>   List devices from cloud account")
	fmt.Fprintln(os.Stderr, "  meross-cli discover                              Discover devices on local network")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Cloud Options:")
	fmt.Fprintln(os.Stderr, "  -email       Account email address")
	fmt.Fprintln(os.Stderr, "  -password    Account password")
	fmt.Fprintln(os.Stderr, "  -url         API base URL")
	fmt.Fprintln(os.Stderr, "               Refoss EU: https://iotx-eu.refoss.net (default)")
	fmt.Fprintln(os.Stderr, "               Refoss US: https://iotx-us.refoss.net")
	fmt.Fprintln(os.Stderr, "               Meross: https://iot.meross.com")
	fmt.Fprintln(os.Stderr, "  -json        Output in JSON format")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Discover Options:")
	fmt.Fprintln(os.Stderr, "  -uuid        Discover specific device by UUID")
	fmt.Fprintln(os.Stderr, "  -timeout     Discovery timeout in seconds (default: 3)")
	fmt.Fprintln(os.Stderr, "  -json        Output in JSON format")
	fmt.Fprintln(os.Stderr, "  -verbose     Show debug output")
}

func runCloud(args []string) {
	cloudFlags := flag.NewFlagSet("cloud", flag.ExitOnError)
	email := cloudFlags.String("email", "", "Meross/Refoss account email")
	password := cloudFlags.String("password", "", "Meross/Refoss account password")
	baseURL := cloudFlags.String("url", "", "API base URL\n    \tRefoss EU: https://iotx-eu.refoss.net (default)\n    \tRefoss US: https://iotx-us.refoss.net\n    \tMeross: https://iot.meross.com")
	jsonOutput := cloudFlags.Bool("json", false, "Output in JSON format")
	cloudFlags.Parse(args)

	if *email == "" || *password == "" {
		printUsage()
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

func main() {
	rand.Seed(time.Now().UnixNano())

	if len(os.Args) > 1 && os.Args[1] == "discover" {
		runDiscover(os.Args[2:])
		return
	}

	// Default: cloud login mode
	runCloud(os.Args[1:])
}
