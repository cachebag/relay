# Relay API

This document describes the HTTP API provided by `relayd`.

The API currently supports:

- Health checks
- Pool creation
- Pool listing
- Node heartbeats
- Node listing
- Filtering nodes by pool


## Health Check

### GET /healthz

Checks whether the relay server is running.

#### Request

No request body is required.

```http
GET /healthz
```

#### Success Response

Status:

```text
200 OK
```

Body:

```text
ok
```


## Create a Pool

### POST /pools

Creates a new pool.

A pool is used to group nodes together. When a pool is created, the server generates an ID and a secret token for the pool.

Nodes use the pool token when sending heartbeats.

#### Request

```http
POST /pools
Content-Type: application/json
```

Example body:

```json
{
  "name": "test-pool"
}
```

The `name` field is required.

#### Success Response

Status:

```text
201 Created
```

Example response:

```json
{
  "id": "pool-id",
  "name": "test-pool",
  "token": "generated-pool-token",
  "created_at": "2026-09-19T12:00:00Z"
}
```

The pool token is returned when the pool is created.

The token should be kept private because nodes use it to authenticate themselves to a pool.

#### Error Responses

If the request body is invalid or the pool name is missing:

```text
400 Bad Request
```

If the pool cannot be stored:

```text
500 Internal Server Error
```


## List Pools

### GET /pools

Returns the existing pools.

#### Request

```http
GET /pools
```

No request body is required.

#### Success Response

Status:

```text
200 OK
```

Example response:

```json
[
  {
    "id": "pool-id",
    "name": "test-pool",
    "token": "",
    "created_at": "2026-09-19T12:00:00Z"
  }
]
```

Pool tokens are not exposed by this endpoint.

The token is only returned when the pool is originally created.

If no pools exist, the response is an empty JSON array:

```json
[]
```

#### Error Response

If the pools cannot be retrieved:

```text
500 Internal Server Error
```


## Node Heartbeat

### POST /nodes/heartbeat

Allows a node to report its information to the relay server.

Each heartbeat must include a valid `pool_token`.

The server uses the token to determine which pool the node belongs to. The node does not choose its own `pool_id`.

#### Request

```http
POST /nodes/heartbeat
Content-Type: application/json
```

Example body:

```json
{
  "id": "node-1",
  "hostname": "test-host",
  "os": "linux",
  "arch": "amd64",
  "cpus": 4,
  "mem_bytes": 8000000000,
  "pool_token": "generated-pool-token"
}
```

### Request Fields

| Field | Description |
| --- | --- |
| `id` | Unique ID of the node |
| `hostname` | Hostname of the node |
| `os` | Operating system running on the node |
| `arch` | CPU architecture |
| `cpus` | Number of CPUs available |
| `mem_bytes` | Amount of memory available in bytes |
| `pool_token` | Secret token belonging to the node's pool |

The `id` field is required.

The `pool_token` must match an existing pool.

When a valid token is provided, the server looks up the pool and assigns its ID to the node.

The server also sets the node's `last_seen` time when the heartbeat is received.

#### Success Response

Status:

```text
204 No Content
```

A successful heartbeat does not return a response body.

#### Error Responses

If the JSON request is malformed:

```text
400 Bad Request
```

If the node ID is missing:

```text
400 Bad Request
```

If the pool token is invalid:

```text
403 Forbidden
```

If the node cannot be stored:

```text
500 Internal Server Error
```


## List Nodes

### GET /nodes

Returns all registered nodes.

#### Request

```http
GET /nodes
```

#### Success Response

Status:

```text
200 OK
```

Example response:

```json
[
  {
    "id": "node-1",
    "hostname": "test-host",
    "os": "linux",
    "arch": "amd64",
    "cpus": 4,
    "mem_bytes": 8000000000,
    "pool_id": "pool-id",
    "last_seen": "2026-09-19T12:00:00Z"
  }
]
```

If no nodes have been registered, the endpoint returns:

```json
[]
```

#### Error Response

If the nodes cannot be retrieved:

```text
500 Internal Server Error
```


## List Nodes by Pool

### GET /nodes?pool=<pool-id>

The `pool` query parameter can be used to return only nodes belonging to a specific pool.

#### Request

Example:

```http
GET /nodes?pool=pool-id
```

#### Success Response

Status:

```text
200 OK
```

Only nodes whose `pool_id` matches the requested pool are returned.

Example:

```json
[
  {
    "id": "node-1",
    "hostname": "test-host",
    "os": "linux",
    "arch": "amd64",
    "cpus": 4,
    "mem_bytes": 8000000000,
    "pool_id": "pool-id",
    "last_seen": "2026-09-19T12:00:00Z"
  }
]
```

If the requested pool has no registered nodes, the response is:

```json
[]
```

If the `pool` query parameter is not provided:

```http
GET /nodes
```

all registered nodes are returned.