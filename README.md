# Minimal WebSocket Backend

This repository provides a minimal Go backend for a two‑peer WebSocket communication. It establishes rooms based on a UUID `appID`, accepts only two connections per room, and broadcasts messages directly between peers. Noisytransfer is used for e2ee synchronous transfer of data in the [noisytransfer-app](https://github.com/collapsinghierarchy/noisytransferapp).

## Features

* **Room management**: Map connections to `appID` rooms (max 2 peers).
* **UUID validation**: Reject invalid `appID` parameters.
* **Origin whitelist**: Only allow WebSocket upgrades from configured origins.
* **Direct broadcast**: Relay text messages from one peer to the other with no intermediate queue.

## Repository Structure

```text
├── hub.go      # Core Hub implementation (room registration, broadcast)
├── handler.go  # HTTP handler for WebSocket endpoint (/ws)
└── main.go     # Application entry point (wire Hub + handler)
```

## Prerequisites

* Go 1.24.3

## Installation & Setup

1. **Clone the repo**
Clone the repo and type `go mod tidy` to pull the dependencies.

2. **Configure allowed origins**

   In `main.go`, update the `allowedOrigins` slice with the domains you trust:

   ```go
   allowed := []string{
     "https://app.example.com",
     "https://dashboard.example.com",
   }
   ```
  If you want to integrate the endpoints into your back-end, then simply use the main.go as an example and integrate it in the same way.


## Running the Server

```bash
cd noisybufferd
go run main.go
```

By default, the server listens on port `1234`. You can modify `main.go` to change the address.

## Usage

On the client side (e.g. your Quasar app), open a WebSocket connection:

```js
const ws = new WebSocket(
  `wss://api.example.com/ws?appID=${appId}`
);

ws.onmessage = event => {
  const msg = JSON.parse(event.data);
  // process incoming message
};

// Send a message to the peer:
ws.send(JSON.stringify({ type: 'data', payload: '...' }));
```

* **Handshake**: The server upgrades the HTTP GET `/ws?appID=…` request to WebSocket after origin & UUID checks.
* **Broadcast**: Messages sent by one connection are forwarded to the other peer in the same `appID` room.

## Notes

* Only **text** frames are supported. Binary messages are ignored.
* If a room already has two peers, additional connections receive an error and are closed.

# Have fun!

