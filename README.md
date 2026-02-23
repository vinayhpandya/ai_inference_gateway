# ai_inference_gateway

This is a bare minimum API gateway with some validation and model routing
## Run

```bash
go run main.go
```

The server starts on port 8080 by default. Set the `PORT` environment variable to use a different port.

## Setting enviroment variables
Run `LLAMA_CPP_SERVER` on port 8081 and have it as export it as an enviromnet variable before running the script

## TDOD
1. Rate limiting
2. Circuit breaker and intelligent routing
