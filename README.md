# meross-cli

A command-line tool to list devices from Meross and Refoss smart home accounts.

## Installation

```bash
go build -o meross-cli .
```

## Usage

```bash
meross-cli -email <email> -password <password> [options]
```

### Options

| Flag | Description |
|------|-------------|
| `-email` | Account email address |
| `-password` | Account password |
| `-url` | API base URL (see below) |
| `-json` | Output in JSON format |

### API URLs

| Service | Region | URL |
|---------|--------|-----|
| Refoss | EU | `https://iotx-eu.refoss.net` (default) |
| Refoss | US | `https://iotx-us.refoss.net` |
| Meross | Global | `https://iotx.meross.com` |

## Examples

### List devices (Refoss EU)

```bash
./meross-cli -email user@example.com -password mypassword
```

```
Logging in as user@example.com...
Login successful!
Fetching devices...

Found 2 device(s):

1. Living Room Plug
   UUID:     2201063512345678901234567890abcd
   Type:     mss310
   Status:   online
   Firmware: 3.2.7
   Hardware: 3.0.0
   Region:   eu
   Domain:   eu-iotx.meross.com
   Key:      abcdef1234567890abcdef1234567890

2. Kitchen Power Strip
   UUID:     2201063587654321098765432109fedc
   Type:     mss425e
   Status:   online
   Firmware: 3.1.4
   Hardware: 2.0.0
   Region:   eu
   Domain:   eu-iotx.meross.com
   Key:      abcdef1234567890abcdef1234567890
   Channels:
     - [1] Coffee Maker
     - [2] Toaster
     - [3] Microwave
```

### JSON output

```bash
./meross-cli -email user@example.com -password mypassword -json
```

```json
[
  {
    "uuid": "2201063512345678901234567890abcd",
    "devName": "Living Room Plug",
    "onlineStatus": 1,
    "fmwareVersion": "3.2.7",
    "hdwareVersion": "3.0.0",
    "deviceType": "mss310",
    "region": "eu",
    "domain": "eu-iotx.meross.com",
    "channels": []
  }
]
```

### Using with Meross account

```bash
./meross-cli -email user@example.com -password mypassword -url https://iotx.meross.com
```

### Using with Refoss US account

```bash
./meross-cli -email user@example.com -password mypassword -url https://iotx-us.refoss.net
```
