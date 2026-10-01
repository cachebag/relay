# Relay API

This document describes the HTTP API provided by `relayd`.

The API currently supports:

- Health checks
- Pool creation
- Pool listing
- Node heartbeats
- Node listing
- Filtering nodes by pool
- Job creation
- Job lookup by ID
- Job listing


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


## Create a Job

### POST /jobs

Creates a new job and places it in the queued state.

The client provides the command to run. The server generates the job ID, sets the initial status, and records the creation time.

#### Request

```http
POST /jobs
Content-Type: application/json
```

Example body:

```json
{
  "command": ["echo", "hi"]
}
```

The `command` field is required and must contain at least one value.

#### Success Response

Status:

```text
201 Created
```

Example response:

```json
{
  "id": "40ca4109f082",
  "status": "queued"
}
```

#### Error Responses

If the JSON request is malformed or the command is empty:

```text
400 Bad Request
```

If the job cannot be stored:

```text
500 Internal Server Error
```


## Get a Job

### GET /jobs/{id}

Returns a specific job using its ID.

#### Request

Example:

```http
GET /jobs/40ca4109f082
```

No request body is required.

#### Success Response

Status:

```text
200 OK
```

Example response:

```json
{
  "id": "40ca4109f082",
  "command": ["echo", "hi"],
  "status": "queued",
  "node_id": "",
  "attempts": 0,
  "created_at": "2026-09-23T15:01:03.320292Z"
}
```

`node_id` is the node the scheduler placed the job on, or empty while the job is queued.

`attempts` counts how many times the scheduler has placed the job on a node. If a node stops heartbeating for 30 seconds while holding an `assigned` or `running` job, the scheduler clears `node_id` and puts the job back to `queued` so the next tick can place it elsewhere, which bumps `attempts` again. After 3 placements the job is marked `failed` instead of being requeued.

#### Error Responses

If the requested job does not exist:

```text
404 Not Found
```

If the job cannot be retrieved:

```text
500 Internal Server Error
```


## List Jobs

### GET /jobs

Returns all stored jobs.

#### Request

```http
GET /jobs
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
    "id": "40ca4109f082",
    "command": ["echo", "hi"],
    "status": "queued",
    "node_id": "",
    "attempts": 0,
    "created_at": "2026-09-23T15:01:03.320292Z"
  }
]
```

If no jobs exist, the response is an empty JSON array:

```json
[]
```

#### Error Response

If the jobs cannot be retrieved:

```text
500 Internal Server Error
```


## Manual Job Endpoint Testing

Start the Relay server before testing the job endpoints:

```bash
go run ./cmd/relayd
```

The server should be available at:

```text
http://localhost:8080
```


### Linux and macOS

#### Create a Job

```bash
curl -s -X POST http://localhost:8080/jobs \
  -H 'Content-Type: application/json' \
  -d '{"command":["echo","hi"]}'
```

Example response:

```json
{"id":"40ca4109f082","status":"queued"}
```

Copy the returned job ID for the next command.

#### Get a Job by ID

Replace `JOB_ID` with the returned job ID:

```bash
curl -s http://localhost:8080/jobs/JOB_ID
```

#### List All Jobs

```bash
curl -s http://localhost:8080/jobs
```

#### Test a Missing Job

```bash
curl -i http://localhost:8080/jobs/doesnotexist
```

Expected status:

```text
404 Not Found
```


### Windows PowerShell

PowerShell and `curl.exe` can handle inline JSON quoting differently, so the request body can be stored in a temporary JSON file.

#### Create the Request File

```powershell
'{"command":["echo","hi"]}' | Set-Content -NoNewline job.json
```

Verify the file contents:

```powershell
Get-Content job.json
```

Expected:

```json
{"command":["echo","hi"]}
```

#### Create a Job

```powershell
curl.exe -s -X POST http://localhost:8080/jobs -H "Content-Type: application/json" --data-binary "@job.json"
```

Example response:

```json
{"id":"40ca4109f082","status":"queued"}
```

Copy the returned job ID.

#### Get a Job by ID

Replace `JOB_ID` with the returned job ID:

```powershell
curl.exe -s http://localhost:8080/jobs/JOB_ID
```

#### List All Jobs

```powershell
curl.exe -s http://localhost:8080/jobs
```

#### Test a Missing Job

```powershell
curl.exe -i http://localhost:8080/jobs/doesnotexist
```

Expected status:

```text
404 Not Found
```

The temporary JSON file can be removed after testing:

```powershell
Remove-Item job.json
```


## Automated Testing

Run the complete test suite from the repository root:

```bash
go test ./...
```

The job API tests verify that:

- Jobs can be created with `POST /jobs`.
- New jobs receive a generated ID and begin with the `queued` status.
- Jobs without a command are rejected with `400 Bad Request`.
- Jobs can be retrieved by ID with `GET /jobs/{id}`.
- Unknown job IDs return `404 Not Found`.
- All jobs can be retrieved with `GET /jobs`.
- A fresh job store returns an empty JSON array.

### POST /jobs/{id}/result

Submit a job result from an agent.

**Request body:**
```json
{
  "node_id": "node-123",
  "attempt": 1,
  "output": "job result data"
}
```

**Response:**
- `204 No Content` - Result accepted, job marked done
- `409 Conflict` - Stale result (job was reassigned to another node)

---

### GET /nodes

List all nodes with their current online/offline status.

**Query parameters:**
- `pool` - Filter by pool name (optional)

**Response:**
```json
[
  {
    "id": "node-1",
    "pool": "default",
    "status": "online",
    "last_seen": "2026-09-30T13:54:00Z"
  },
  {
    "id": "node-2",
    "pool": "default",
    "status": "offline",
    "last_seen": "2026-09-30T13:00:00Z"
  }
]
```

**Status values:**
- `online` - Node seen within the last 60 seconds
- `offline` - Node not seen for 60+ seconds