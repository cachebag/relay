
# Relay API

This document describes the HTTP API provided by `relayd`.

The API currently supports:

- Health checks
- Pool creation
- Pool listing
- Pool lookup by token
- Pool join-token rotation
- Pool credit balances
- Node heartbeats
- Node listing
- Filtering nodes by pool
- Removing a node that leaves its pool
- Pool-authenticated job creation
- Job ownership and submitter tracking
- Job lookup by ID
- Job listing and filtering by pool
- Pool-aware job scheduling
- Handing a node its next job
- Accepting job results
- Serving job output

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

A pool is used to group nodes and jobs together. When a pool is created, the server generates an ID and a secret join token for the pool.

Nodes use the pool join token when sending heartbeats. Users also use the join token to submit jobs to that pool.

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
  "created_at": "2026-09-19T12:00:00Z",
  "admin_token": "generated-admin-token"
}
```

The pool join token and admin token are returned when the pool is created.

The join token allows nodes to join the pool and users to submit jobs. The admin token allows authorized pool management operations, such as rotating the join token.

Both tokens should be kept private.

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

If no pools exist, the response is an empty JSON array:

```json
[]
```

#### Error Response

If the pools cannot be retrieved:

```text
500 Internal Server Error
```

## Get Own Pool

### GET /pools/self

Returns the pool that owns the join token in the `Authorization` header.

The CLI uses this endpoint to check a token without registering a node.

#### Request

```http
GET /pools/self
Authorization: Bearer <pool-token>
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
  "id": "pool-id",
  "name": "test-pool",
  "created_at": "2026-09-19T12:00:00Z"
}
```

The token is never included in the response.

#### Error Responses

If the header is missing, is not a Bearer token, or the token does not belong to any pool:

```text
403 Forbidden
```

If the pool cannot be retrieved:

```text
500 Internal Server Error
```

## Rotate Pool Join Token

### POST /pools/{id}/token

Generates a new join token for an existing pool.

This operation requires the pool's admin token rather than its join token.

After rotation, the previous join token is no longer valid for joining the pool or submitting new jobs.

#### Request

```http
POST /pools/pool-id/token
Authorization: Bearer <admin-token>
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
  "token": "new-generated-pool-token"
}
```

#### Error Responses

If the admin token is missing or invalid:

```text
401 Unauthorized
```

If the pool does not exist:

```text
404 Not Found
```

If the pool cannot be retrieved or updated:

```text
500 Internal Server Error
```

## Get Pool Credit Balances

### GET /pools/{id}/credits

Returns every account's credit balance in the pool, highest balance first. Accounts with the same balance are sorted by name.

Owners earn 1 credit for every minute their machine is online. Credits are written to the ledger in blocks of 10 minutes, plus whatever whole minutes are left when the machine goes offline, so a balance can lag behind by up to 10 minutes.

#### Request

The pool's join token is sent as a bearer token.

```http
GET /pools/pool-id/credits
Authorization: Bearer generated-pool-token
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
    "account": "akrm",
    "balance": 42
  },
  {
    "account": "omar",
    "balance": 10
  }
]
```

A pool with no credits yet returns an empty array.

#### Error Responses

If the `Authorization` header is missing, the token doesn't match any pool, or the token belongs to a different pool than `{id}`:

```text
403 Forbidden
```

If the balances cannot be read:

```text
500 Internal Server Error
```


## Node Heartbeat

### POST /nodes/heartbeat

Allows a node to report its information to the Relay server.

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
  "owner": "akrm",
  "pool_token": "generated-pool-token"
}
```

#### Request Fields

| Field | Description |
| --- | --- |
| `id` | Unique ID of the node |
| `hostname` | Hostname of the node |
| `os` | Operating system running on the node |
| `arch` | CPU architecture |
| `cpus` | Number of CPUs available |
| `mem_bytes` | Amount of memory available in bytes |
| `owner` | Name of the person who earns credits for this node's online time |
| `pool_token` | Secret join token belonging to the node's pool |

The `id` field is required.

The `pool_token` must match an existing pool.

When a valid token is provided, the server looks up the pool and assigns its ID to the node.

The server also sets the node's `last_seen` time when the heartbeat is received.

While a node is online, its `owner` earns 1 credit per minute in the node's pool. A node with no `owner` earns nothing.

#### Success Response

Status:

```text
204 No Content
```

A successful heartbeat does not return a response body.

#### Error Responses

If the JSON request is malformed or the node ID is missing:

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

Returns all registered nodes with their current online/offline status.

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
    "owner": "akrm",
    "last_seen": "2026-09-19T12:00:00Z",
    "status": "online"
  },
  {
    "id": "node-2",
    "hostname": "old-host",
    "os": "darwin",
    "arch": "arm64",
    "cpus": 8,
    "mem_bytes": 16000000000,
    "pool_id": "pool-id",
    "owner": "akrm",
    "last_seen": "2026-09-19T11:00:00Z",
    "status": "offline"
  }
]
```

`status` is computed by the server on each request:

| Value | Meaning |
| --- | --- |
| `online` | The node's last heartbeat was within the last 30 seconds |
| `offline` | The node has not sent a heartbeat in 30 seconds or more |

The 30-second window is `store.NodeTimeout`, the same value the scheduler uses to decide whether a node can receive jobs.

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
    "owner": "akrm",
    "last_seen": "2026-09-19T12:00:00Z",
    "status": "online"
  }
]
```

If the requested pool has no registered nodes, the response is:

```json
[]
```

If the `pool` query parameter is not provided, all registered nodes are returned.

## Leave a Pool

### DELETE /nodes/{id}

Removes a node from its pool.

`relay-agent leave` calls this endpoint when a contributor takes their machine out of a pool.

Any job that is `assigned` or `running` on the node goes back to `queued` with its `node_id` cleared.

The scheduler can then assign the job to another online node belonging to the same pool.

The scheduler counts an attempt each time it places a job. Leaving gives that attempt back, so voluntarily leaving does not use one of the job's three attempts.

The node record is then deleted, causing the node to disappear from `GET /nodes` immediately.

If the agent is still running, its next heartbeat registers the node again, so the agent should be stopped first.

#### Request

The pool's join token is sent as a bearer token.

```http
DELETE /nodes/node-1
Authorization: Bearer generated-pool-token
```

No request body is required.

#### Success Response

Status:

```text
204 No Content
```

#### Error Responses

If the authorization header is missing, the token does not match any pool, or the token belongs to a different pool than the node:

```text
403 Forbidden
```

If no node has that ID:

```text
404 Not Found
```

`relay-agent leave` treats a 404 as success because it means the node is already gone.

If the node or its jobs cannot be read or updated:

```text
500 Internal Server Error
```

## Create a Job

### POST /jobs

Creates a new job in the pool associated with the provided join token and places it in the queued state.

The request must include a valid pool join token in the `Authorization` header, along with a command and the name of the person submitting the job.

The server generates the job ID, determines the pool from the token, sets the initial status, and records the creation time.

#### Request

```http
POST /jobs
Authorization: Bearer <pool-join-token>
Content-Type: application/json
```

Example body:

```json
{
  "command": ["echo", "hi"],
  "submitter": "alice"
}
```

#### Request Fields

| Field | Description |
| --- | --- |
| `command` | Command and arguments to execute. Must contain at least one value |
| `submitter` | Name of the person submitting the job. Cannot be empty or whitespace |

The client does not select the job's `pool_id`.

Instead, the server looks up the pool associated with the authenticated join token and assigns the pool's ID to the job.

Any `pool_id` supplied in the request body is ignored.

The CLI automatically sends the saved pool token and submitter name from the user's login profile when running `relay submit`.

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

The created job is stored with the authenticated pool ID and submitter name.

The scheduler can only assign the job to an online node belonging to the same pool.

#### Error Responses

If the JSON request is malformed, the command is empty, or the submitter is missing or blank:

```text
400 Bad Request
```

If the authorization header is missing, is not a Bearer token, or contains an unknown pool join token:

```text
403 Forbidden
```

If the pool or job cannot be retrieved or stored:

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
  "pool_id": "pool-id",
  "submitter": "alice",
  "attempts": 0,
  "created_at": "2026-09-23T15:01:03.320292Z"
}
```

`pool_id` identifies the pool that owns the job. The server determines this value from the join token used during submission.

`submitter` records the name provided by the user who created the job.

`node_id` is the node the scheduler assigned the job to, or empty while the job is queued.

`attempts` counts how many times the scheduler has placed the job on a node.

`started_at` appears once a node has picked the job up and `finished_at` once a result was accepted or the scheduler gave up. If the job is put back in the queue, `started_at` is cleared so it always covers the current attempt. When a result is accepted, the submitter is charged 1 credit for every minute between the two, rounded up (minimum 1). A job the scheduler failed after 3 lost nodes never reported, so it gets `finished_at` but no charge.

If a node stops heartbeating for 30 seconds while holding an `assigned` or `running` job, the scheduler clears `node_id` and puts the job back into the queue.

The next scheduling attempt can only place the job onto another node belonging to the same pool.

After three unsuccessful placements due to node loss, the job is marked `failed` instead of being requeued.

Jobs created before pool ownership was introduced may have empty `pool_id` and `submitter` fields.

Legacy jobs without a pool ID cannot be scheduled onto a node in an existing pool. These jobs can be cleared using `tools/resetdb`.

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

Returns all stored jobs, including the pool each job belongs to and the name of the person who submitted it.

An optional `pool` query parameter filters the results to a specific pool.

#### Request

To retrieve all jobs:

```http
GET /jobs
```

To retrieve jobs belonging to one pool:

```http
GET /jobs?pool=<pool-id>
```

No request body or authentication is required.

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
    "pool_id": "pool-a",
    "submitter": "alice",
    "attempts": 0,
    "created_at": "2026-09-23T15:01:03.320292Z"
  },
  {
    "id": "51db5210a193",
    "command": ["sleep", "5"],
    "status": "assigned",
    "node_id": "node-2",
    "pool_id": "pool-b",
    "submitter": "bob",
    "attempts": 1,
    "created_at": "2026-09-23T15:02:00Z"
  }
]
```

When the `pool` parameter is provided, only jobs whose `pool_id` matches the requested value are returned.

For example:

```http
GET /jobs?pool=pool-a
```

returns only the job submitted by `alice` in the example above.

If no jobs match the requested pool, or no jobs exist, the response is an empty JSON array:

```json
[]
```

The `relay jobs` CLI displays a `SUBMITTER` column using the job's stored submitter name.

Legacy jobs without submitter information display `-`.

#### Error Response

If the jobs cannot be retrieved:

```text
500 Internal Server Error
```

## Pool-Aware Job Scheduling

The scheduler only assigns queued jobs to online nodes belonging to the same pool.

Each job has a `pool_id` assigned during submission, and each node has a `pool_id` assigned during registration.

A job can only be placed on a node when both pool IDs match.

For example:

| Job | Job Pool | Available Node | Result |
| --- | --- | --- | --- |
| Job A | Pool A | Node A in Pool A | Assigned |
| Job A | Pool A | Node B in Pool B | Remains queued |
| Job B | Pool B | Node B in Pool B | Assigned |

When no online nodes are available in a job's pool, the job remains queued even if another pool has idle nodes.

Round-robin assignment operates independently for each pool within a scheduling tick.

Jobs created before the introduction of pool ownership have an empty `pool_id` and cannot be assigned to nodes.

## Submit a Job Result

### POST /jobs/{id}/result

Reports that a job has finished on the node it was assigned to, including its exit code and output.

The server only accepts the result if it comes from the node currently holding the job, for the job's current attempt.

This prevents a node that went offline, and whose job was requeued and assigned elsewhere, from overwriting a newer result.

#### Request

Example:

```http
POST /jobs/40ca4109f082/result
Content-Type: application/json
```

Example body:

```json
{
  "node_id": "node-1",
  "attempt": 1,
  "exit_code": 0,
  "stdout": "hi\n",
  "stderr": ""
}
```

#### Request Fields

| Field | Description |
| --- | --- |
| `node_id` | ID of the node reporting the result |
| `attempt` | The job's `attempts` value when the node received it |
| `exit_code` | Exit code of the job. Required |
| `stdout` | Standard output of the job |
| `stderr` | Standard error of the job |

The body may be at most 16 MiB.

#### Success Response

Status:

```text
204 No Content
```

The result was accepted and stored.

The job is marked `done` if `exit_code` is 0, otherwise `failed`.

No response body is returned.

#### Error Responses

If the JSON request is malformed or `exit_code` is missing:

```text
400 Bad Request
```

If the requested job does not exist:

```text
404 Not Found
```

If the request body is larger than 16 MiB:

```text
413 Request Entity Too Large
```

If the result is stale:

```text
409 Conflict
```

A result is stale when any of the following is true:

- The job's status is not `assigned` or `running`.
- `node_id` does not match the job's current `node_id`.
- `attempt` does not match the job's current `attempts`.

If the job cannot be retrieved or updated:

```text
500 Internal Server Error
```

## Get a Node's Next Job

### GET /nodes/{id}/jobs/next

Returns the oldest job assigned to the node that is still `assigned` or `running`.

A node calls this endpoint to pick up work.

#### Request

Example:

```http
GET /nodes/node-1/jobs/next
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
  "status": "running",
  "node_id": "node-1",
  "pool_id": "pool-a",
  "submitter": "alice",
  "attempts": 1,
  "created_at": "2026-09-23T15:01:03.320292Z",
  "started_at": "2026-09-23T15:01:05Z"
}
```

If the job was `assigned`, it is set to `running` and `started_at` is set to now before the response is sent. A job that is already `running` keeps its `started_at`.

The `attempts` value is not changed.

A `running` job is returned again on subsequent calls until a result is reported.

A node that restarts mid-job can retrieve its work again this way.

If there is nothing for the node:

```text
204 No Content
```

#### Error Response

If the jobs cannot be retrieved or updated:

```text
500 Internal Server Error
```

## Get Job Output

### GET /jobs/{id}/output

Returns the output a node reported for a finished job.

`relay logs` calls this endpoint.

#### Request

Example:

```http
GET /jobs/40ca4109f082/output
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
  "stdout": "hi\n",
  "stderr": "",
  "exit_code": 0
}
```

The output is available once the job is `done` or `failed`.

A job the scheduler failed after three attempts never reported a result, so it has no output.

The response has empty `stdout` and `stderr` and an `exit_code` of `-1`, so `relay logs` exits non-zero.

#### Error Responses

If the requested job does not exist:

```text
404 Not Found
```

If the job is still `queued`, `assigned`, or `running`:

```text
409 Conflict
```

If the job cannot be retrieved:

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

These examples assume the default port. Replace `8080` with the configured port if necessary.

Job creation requires a valid pool join token and a submitter name.

### Linux and macOS

#### Create a Pool

```bash
curl -s -X POST http://localhost:8080/pools \
  -H 'Content-Type: application/json' \
  -d '{"name":"test-pool"}'
```

Copy the `token` value from the response.

Set the pool token:

```bash
POOL_TOKEN="your-pool-token"
```

#### Create a Job

```bash
curl -s -X POST http://localhost:8080/jobs \
  -H "Authorization: Bearer $POOL_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"command":["echo","hi"],"submitter":"alice"}'
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

Verify that the response contains the correct `pool_id` and `submitter`.

#### List All Jobs

```bash
curl -s http://localhost:8080/jobs
```

#### List Jobs by Pool

Replace `POOL_ID` with the ID returned when the pool was created:

```bash
curl -s "http://localhost:8080/jobs?pool=POOL_ID"
```

Only jobs belonging to that pool should appear.

#### Test a Missing Token

```bash
curl -i -X POST http://localhost:8080/jobs \
  -H 'Content-Type: application/json' \
  -d '{"command":["echo","hi"],"submitter":"alice"}'
```

Expected status:

```text
403 Forbidden
```

#### Test an Invalid Token

```bash
curl -i -X POST http://localhost:8080/jobs \
  -H 'Authorization: Bearer invalid-token' \
  -H 'Content-Type: application/json' \
  -d '{"command":["echo","hi"],"submitter":"alice"}'
```

Expected status:

```text
403 Forbidden
```

#### Test an Empty Submitter

```bash
curl -i -X POST http://localhost:8080/jobs \
  -H "Authorization: Bearer $POOL_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"command":["echo","hi"],"submitter":""}'
```

Expected status:

```text
400 Bad Request
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

PowerShell and `curl.exe` can handle inline JSON quoting differently, so request bodies can be stored in temporary JSON files.

#### Create a Pool

Create the request file:

```powershell
'{"name":"test-pool"}' | Set-Content -NoNewline pool.json
```

Create the pool:

```powershell
curl.exe -s -X POST http://localhost:8080/pools -H "Content-Type: application/json" --data-binary "@pool.json"
```

Copy the generated pool token and ID.

Set them as variables:

```powershell
$PoolToken = "your-pool-token"
$PoolID = "your-pool-id"
```

#### Create a Job Request File

```powershell
'{"command":["echo","hi"],"submitter":"alice"}' | Set-Content -NoNewline job.json
```

Verify the file contents:

```powershell
Get-Content job.json
```

Expected:

```json
{"command":["echo","hi"],"submitter":"alice"}
```

#### Create a Job

```powershell
curl.exe -s -X POST http://localhost:8080/jobs -H "Authorization: Bearer $PoolToken" -H "Content-Type: application/json" --data-binary "@job.json"
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

Verify that the response includes the authenticated pool ID and `alice` as the submitter.

#### List All Jobs

```powershell
curl.exe -s http://localhost:8080/jobs
```

#### List Jobs by Pool

```powershell
curl.exe -s "http://localhost:8080/jobs?pool=$PoolID"
```

Only jobs belonging to the specified pool should appear.

#### Test a Missing Token

```powershell
curl.exe -i -X POST http://localhost:8080/jobs -H "Content-Type: application/json" --data-binary "@job.json"
```

Expected status:

```text
403 Forbidden
```

#### Test an Invalid Token

```powershell
curl.exe -i -X POST http://localhost:8080/jobs -H "Authorization: Bearer invalid-token" -H "Content-Type: application/json" --data-binary "@job.json"
```

Expected status:

```text
403 Forbidden
```

#### Test an Empty Submitter

Create a request with an empty submitter:

```powershell
'{"command":["echo","hi"],"submitter":""}' | Set-Content -NoNewline empty-submitter.json
```

Submit the request:

```powershell
curl.exe -i -X POST http://localhost:8080/jobs -H "Authorization: Bearer $PoolToken" -H "Content-Type: application/json" --data-binary "@empty-submitter.json"
```

Expected status:

```text
400 Bad Request
```

#### Test a Missing Job

```powershell
curl.exe -i http://localhost:8080/jobs/doesnotexist
```

Expected status:

```text
404 Not Found
```

#### Remove Temporary Request Files

After testing:

```powershell
Remove-Item pool.json, job.json, empty-submitter.json
```

## CLI Job Submission

The `relay submit` command uses the saved login profile to authenticate job submissions.

A profile is created through `relay login` and includes the pool's join token and the user's name.

The CLI automatically includes:

- The pool join token as a Bearer token in the `Authorization` header.
- The user's name as the `submitter` field in the request body.

The client does not need to supply `pool_id`; the server determines it from the token.

### Example

First, log into a pool:

```bash
relay login
```

Then submit a job:

```bash
relay submit echo hello
```

The CLI sends an authenticated request to `POST /jobs`.

If no valid login profile exists, the command exits with code 1 and instructs the user to log in.

If the pool token is rejected, the CLI displays an error instructing the user to log in again.

### List Jobs

```bash
relay jobs
```

The resulting table includes:

```text
ID        STATUS    NODE      ATTEMPTS   AGE  SUBMITTER  COMMAND
abc123    done      node-1           1   12s alice      echo hello
def456    queued    -                0    5s bob        sleep 5
```

An unassigned job displays `-` in the `NODE` column.

Jobs without submitter information display `-` in the `SUBMITTER` column.

To filter the job list by pool:

```bash
relay jobs --pool POOL_ID
```

## Pool Isolation Testing

Pool isolation verifies that jobs are never scheduled onto nodes belonging to a different pool.

The test requires two separate pools with agents.

### Setup

Create two pools:

```text
Pool A
Pool B
```

Register one agent in each pool:

```text
Pool A -> Agent A
Pool B -> Agent B
```

Verify both agents are online.

### Test 1: Jobs Stay in Their Own Pool

Log into Pool A using its join token.

Submit multiple jobs.

The jobs should:

- Record Pool A's ID as `pool_id`.
- Record the logged-in user's name as `submitter`.
- Only be assigned to Agent A.
- Never be assigned to Agent B.

Next, log into Pool B and submit jobs.

These jobs should only run on Agent B.

### Test 2: Pool Without Online Nodes

Stop Agent A while keeping Agent B online.

Submit a job to Pool A.

The job should remain `queued` because Pool A has no online nodes.

Agent B must not receive Pool A's job, even if Agent B is idle.

Start Agent A again.

Once Agent A resumes heartbeating, the queued job should become eligible for scheduling onto Agent A.

### Test 3: Job Filtering

With jobs stored in both pools, request:

```http
GET /jobs?pool=<pool-a-id>
```

Verify that only Pool A's jobs are returned.

Then request:

```http
GET /jobs?pool=<pool-b-id>
```

Verify that only Pool B's jobs are returned.

Finally, request:

```http
GET /jobs
```

Verify that jobs from both pools are returned.

### Test 4: Submitter Tracking

Submit jobs using two different login profiles.

Verify that each created job records the correct submitter name.

Run:

```bash
relay jobs
```

Verify that the `SUBMITTER` column displays the corresponding user for each job.

## Automated Testing

Run the complete Go test suite from the repository root:

```bash
go test ./...
```

The job storage tests verify that:

- Pool IDs and submitter names are saved with jobs.
- Both fields survive retrieving and updating jobs.
- Both fields survive listing jobs.
- SQLite preserves both fields across database restarts.
- Existing SQLite databases can be migrated without losing stored jobs.

The job API tests verify that:

- Jobs can be created with a valid pool join token.
- Missing or invalid tokens return `403 Forbidden`.
- Jobs without a command return `400 Bad Request`.
- Jobs without a submitter return `400 Bad Request`.
- Jobs receive the pool ID associated with their authenticated token.
- A client cannot override the authenticated pool by supplying `pool_id`.
- Jobs can be retrieved by ID.
- Unknown job IDs return `404 Not Found`.
- Jobs can be filtered using `GET /jobs?pool=<pool-id>`.
- A fresh job store returns an empty JSON array.

The scheduler tests verify that:

- Queued jobs are assigned only to online nodes.
- Jobs are assigned only to nodes belonging to their pool.
- Jobs remain queued if their pool has no online nodes.
- Round-robin scheduling operates independently within each pool.
- Jobs without a pool ID cannot be scheduled.
- Jobs assigned to offline nodes can be requeued and reassigned.
- Jobs fail after reaching the maximum number of placement attempts.

### Python CLI Tests

From the repository root:

```bash
cd cli
uv run pytest tests -v
```

The CLI tests verify that:

- `relay submit` includes the pool join token in the Authorization header.
- `relay submit` includes the name from the saved login profile.
- Submitting without a valid profile exits with code 1.
- Invalid pool tokens produce an appropriate error.
- `relay jobs` displays the `SUBMITTER` column.
- Jobs without submitter information display `-`.
- Pool filtering sends the correct query parameter.

Run both the Go and Python test suites before merging changes.
