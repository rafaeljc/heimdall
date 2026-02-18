# Heimdall Node.js SDK

[![npm version](https://img.shields.io/npm/v/@heimdall/node-sdk.svg?style=flat-square)](https://www.npmjs.com/package/@heimdall/node-sdk)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![TypeScript](https://img.shields.io/badge/TypeScript-5.5+-blue.svg)](https://www.typescriptlang.org/)
[![Node.js 20+](https://img.shields.io/badge/Node.js-20+-green.svg)](https://nodejs.org/)

The official Node.js SDK for **Heimdall**, a high-performance, open-source Feature Flagging platform.

This SDK connects to the Heimdall Data Plane via **gRPC** for ultra-low latency flag evaluation and includes a built-in **L1 Cache (LRU)** for maximum efficiency.

---

## Features

- **High Performance:** Uses gRPC/Protobuf for efficient binary communication.
- **Smart Caching:** Built-in LRU (Least Recently Used) cache (L1) to serve repeated evaluations instantly.
- **Type-Safe:** Written in strict TypeScript with comprehensive type definitions.
- **Secure:** Enforces authentication via API Keys and supports SSL/TLS.
- **Resilient:** Automatic timeout handling, fail-fast validation, and safe defaults.

---

## Installation

```bash
npm install @heimdall/node-sdk
# or
yarn add @heimdall/node-sdk
```

---

## Quick Start

### 1. Initialize the Client

Ideally, create a singleton instance of the client at the start of your application.

```typescript
import { HeimdallClient } from '@heimdall/node-sdk';

const client = new HeimdallClient({
  // The address of your Heimdall Data Plane (gRPC)
  target: process.env.HEIMDALL_TARGET || 'localhost:50051',
  
  // Your Environment API Key (e.g., Production or Staging key)
  apiKey: process.env.HEIMDALL_API_KEY,
});
```

### 2. Evaluate a Flag

Use `getEvaluation` to check a feature flag status. You must provide a **default value** which is returned in case of errors (network down, invalid key, etc.).

```typescript
// Context data used for targeting rules (e.g., User ID, Email, Role, Region)
// Context can be any object; server rules determine which fields matter
const userContext = {
  userId: 'user-123',
  role: 'beta-tester',
  region: 'us-east-1',
  // Add any other attributes needed for targeting rules
};

// Evaluate the flag 'new-checkout-flow'
const result = await client.getEvaluation(
  'new-checkout-flow', // Flag Key
  userContext,         // Context for rules
  false                // Default Value (Fallback)
);

if (result.value) {
  console.log('Showing new feature');
} else {
  console.log('Showing legacy feature');
}

// You can also inspect the reason for the decision
// e.g., "RULE_MATCH", "DEFAULT_VALUE", "CACHE_HIT"
console.log(`Decision Reason: ${result.reason}`);
```

### Handle Errors Gracefully

The SDK returns a **default value** on errors (network failures, auth issues, timeouts). You can also catch and log errors:

```typescript
try {
  const result = await client.getEvaluation('new-checkout-flow', userContext, false);
  console.log('Flag is:', result.value);
} catch (error) {
  console.error('Flag evaluation failed:', error.message);
  // The default value (false) is already returned, so your app doesn't break
}
```

### 3. Graceful Shutdown

When your application stops (e.g., receiving `SIGTERM`), close the client to release gRPC connections and clear the cache.

```typescript
await client.close();
```

---

## Configuration Options

The `HeimdallClient` constructor accepts the following options:

| Option | Type | Required | Default | Description |
| :--- | :--- | :---: | :--- | :--- |
| `target` | `string` | Yes | - | The gRPC server address (host:port). |
| `apiKey` | `string` | Yes | - | The Environment API Key for authentication. |
| `timeout` | `number` | No | `5000` | Request timeout in milliseconds (clamped between 20ms and 10s). |
| `cacheTTL` | `number` | No | `60000` | Cache Time-To-Live in ms. Must be non-negative (≥ 0). Set to `0` to disable caching. |
| `cacheSize` | `number` | No | `1000` | Max number of items in the internal LRU cache. Must be a positive integer (≥ 1). |
| `insecure` | `boolean` | No | `false` | If `true`, disables SSL/TLS (use only for local dev). |
| `logger` | `Logger` | No | `console` | Custom logger implementation (must have info/error/warn/debug). |

---

## Architecture & Caching Strategy

To ensure minimal latency impact on your application, the SDK uses a **Read-Through L1 Cache** strategy:

1.  **Cache Lookup:** When `getEvaluation` is called, the SDK first generates a deterministic hash key based on the *Flag Key* and sorted *Context*.
2.  **Hit:** If the result is in memory and not expired (`cacheTTL`), it returns instantly (0ms network latency).
3.  **Miss:** If not found, it makes a gRPC call to the Heimdall Server.
4.  **Store:** The result from the server is stored in the LRU cache for subsequent calls.

### Disabling Cache
If real-time consistency is critical for every single call (at the cost of latency), you can disable the L1 cache:

```typescript
const client = new HeimdallClient({
  // ... other options
  cacheTTL: 0, // Disables caching completely
});
```

### Cache Lifecycle
The LRU cache is automatically managed by the SDK. To refresh evaluations before TTL expires, create a new client instance. The cache is cleared when you call `client.close()`.

---

## Troubleshooting

### `UNAUTHENTICATED` Error
If you see logs indicating authentication failure:
1. Ensure `apiKey` is correct and active in the Heimdall Console.
2. Verify you are using the correct key for the environment (e.g., using a Dev key against a Prod server).

### `DEADLINE_EXCEEDED`
The SDK defaults to a 5-second timeout. If your network is slow or the Data Plane is far away, you might see this.
* **Solution:** Increase the `timeout` in the constructor, or check the connectivity to the `target`.

---

## License

MIT © [Heimdall](https://github.com/rafaeljc/heimdall)
