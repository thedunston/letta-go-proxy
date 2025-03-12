# letta-go-proxy

Go proxy server for Letta API server.

## Build
```
go mod init pproxy
go mod tidy
go build .
```

## Run the proxy

Run with:

```./pproxy```

or

```./pproxy -api-server http://localhost:8283/v1```

The `-api-server` value provided will be saved to the user's home directory as `letta-api-server.json`

It can then be run with:

```./pproxy``` 

and will read the value from the json file for the `LETTA_API_SERVER`

NOTE: Windows, use the backslash.

```.\pproxy```

## Start with environment variables

You can also set an environment variable:

```export LETTA_API_SERVER="http://localhost:8283/v1"```

Windows

```set LETTA_API_SERVER="http://localhost:8283/v1"```

## Proxy running

By default, the proxy will listen on all interfaces and port `8284`, unless `-host` or `port` are provided.

```./pproxy -host 127.0.0.1 -port 8284```

## Logs
Logs will be printed to the console.

```
2025/03/09 11:57:55 Letta API server set to: http://127.0.0.1:8283/v1
2025/03/09 11:57:55 #################################################
2025/03/09 11:57:55 Point your Letta client to the Proxy server 0.0.0.0:8284
2025/03/09 11:57:57 Request: GET /identities/identity-edee5a48-17ff-4449-b3b6-c66bb28a2a9d
2025/03/09 11:57:57 Headers: map[Accept:[*/*] Accept-Encoding:[gzip, deflate] Connection:[keep-alive] User-Agent:[python-requests/2.32.3]]
2025/03/09 11:57:57 Proxying standard request
2025/03/09 11:57:57 Normalized URL: http://localhost:8283/v1/identities/identity-edee5a48-17ff-4449-b3b6-c66bb28a2a9d
2025/03/09 11:57:57 Original request Content-Type: 
2025/03/09 11:57:57 Original request Content-Length: 
2025/03/09 11:57:57 Read request body (0 bytes): 
2025/03/09 11:57:57 Response status: 200
2025/03/09 11:57:57 Successfully proxied response: status=200, bytes=777
```
