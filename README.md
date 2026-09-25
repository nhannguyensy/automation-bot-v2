# Automation Bot v2

[![Go CI](https://github.com/nhannguyensy/automation-bot-v2/actions/workflows/go.yml/badge.svg)](https://github.com/nhannguyensy/automation-bot-v2/actions/workflows/go.yml)
[![Docker Image CI](https://github.com/nhannguyensy/automation-bot-v2/actions/workflows/docker-image.yml/badge.svg)](https://github.com/nhannguyensy/automation-bot-v2/actions/workflows/docker-image.yml)

A lightweight Slack operations bot written in Go. It turns predefined Slack messages into HTTP API calls and can trigger parameterized Jenkins deployments.

This project demonstrates a practical approach to reducing repetitive operational work: engineers can run approved actions from Slack without remembering endpoint URLs or navigating Jenkins jobs.

> **Project status:** proof of concept. Review the [security and production-readiness notes](#security-and-production-readiness) before using it outside a lab or trusted internal environment.

## What it does

- Receives Slack Events API callbacks at `POST /slack/events`
- Responds to Slack URL-verification challenges
- Ignores messages created by other bots
- Lists configured commands with `list` or `list command`
- Maps static commands to configurable HTTP `GET` or `POST` requests
- Supports Basic Authentication for configured POST tasks
- Triggers dynamic Jenkins jobs with `deploy <service-name> <environment>`
- Reports success or failure back to the originating Slack channel
- Runs locally as a Go process or in a Docker container
- Builds and tests through GitHub Actions

## Architecture

~~~mermaid
flowchart TD
    U["Slack user"] --> S["Slack Events API"]
    S --> B["Go bot<br/>POST /slack/events"]
    B --> R{"Command router"}
    R -->|list| L["Available commands"]
    R -->|configured command| A["HTTP API"]
    R -->|deploy service env| J["Jenkins job"]
    L --> W["Slack response"]
    A --> W
    J --> W
~~~

The handler processes events synchronously. A command is considered successful when the downstream API or Jenkins endpoint returns a `2xx` response.

## Example commands

| Slack message | Result |
|---|---|
| `list` | Lists commands defined under `tasks` in `config.json` |
| `status` | Calls the API configured for the `status` task |
| `restart` | Calls the API configured for the `restart` task |
| `deploy payment-api staging` | Builds the Jenkins URL from `url_format` and triggers the job |
| Any unknown command | Returns a help/error message in Slack |

Static command names are configuration-driven. The examples above can be replaced with commands appropriate for your environment.

## Requirements

- Go 1.20 or later
- A Slack app with a bot token
- A public HTTPS endpoint that Slack can reach
- Network access from the bot to the configured APIs and Jenkins
- Docker, only if you want to run the containerized version

## Quick start

### 1. Clone the repository

~~~bash
git clone https://github.com/nhannguyensy/automation-bot-v2.git
cd automation-bot-v2
~~~

### 2. Create the local configuration

~~~bash
cp config.json_template config.json
~~~

Edit `config.json` with your Slack bot token, approved task endpoints, and Jenkins settings:

~~~json
{
  "slack_token": "xoxb-your-bot-token",
  "tasks": {
    "status": {
      "command": "check_status",
      "url": "https://ops-api.example.com/services/status",
      "method": "GET"
    },
    "restart": {
      "command": "restart_service",
      "url": "https://ops-api.example.com/services/restart",
      "method": "POST",
      "user": "automation-bot",
      "token": "replace-me"
    }
  },
  "jenkins": {
    "user": "automation-bot",
    "token": "replace-me",
    "url_format": "https://jenkins.example.com/job/{service-name}/job/{env}/build"
  }
}
~~~

The placeholders `{service-name}` and `{env}` are replaced with the two arguments supplied to the deploy command.

### 3. Run locally

~~~bash
go mod download
go run .
~~~

The service listens on port `8081`:

~~~text
http://localhost:8081/slack/events
~~~

For local development, use a secure tunnel or reverse proxy to expose this endpoint to Slack over HTTPS.

## Slack app configuration

1. Create a Slack app and add a bot user.
2. Add the minimum OAuth scopes required for the channels you use. The bot needs permission to receive the chosen message events and `chat:write` to reply.
3. Enable **Event Subscriptions**.
4. Set the request URL to:
   
   ~~~text
   https://your-bot.example.com/slack/events
   ~~~
5. Subscribe to the required bot message event for your channel type, such as `message.channels`.
6. Install or reinstall the app in the workspace.
7. Invite the bot to the channel where it will receive commands.
8. Copy the `xoxb-...` token into your local configuration.

Slack event permissions vary by public channel, private channel, group message, and direct message. Grant only the event scopes you actually need.

## Run with Docker

The current Dockerfile expects `config.json` to exist during the image build:

~~~bash
cp config.json_template config.json
docker build -t automation-bot-v2 .
docker run --rm -p 8081:8081 automation-bot-v2
~~~

You can replace the packaged configuration at runtime with a read-only bind mount:

~~~bash
docker run --rm \
  -p 8081:8081 \
  -v "$PWD/config.json:/app/config.json:ro" \
  automation-bot-v2
~~~

> **Important:** the current Dockerfile copies `config.json` into the image. Do not publish an image built with real credentials. For production, inject secrets at runtime and remove the configuration copy from the image build.

## Configuration reference

### Top-level settings

| Field | Required | Description |
|---|---:|---|
| `slack_token` | Yes | Slack bot token used to post responses |
| `tasks` | No | Map of exact Slack messages to static HTTP tasks |
| `jenkins` | For deploy | Jenkins authentication and dynamic job URL template |

### Task settings

| Field | Required | Description |
|---|---:|---|
| `command` | Yes | Human-readable operation name used in the Slack response |
| `url` | Yes | Downstream API endpoint |
| `method` | Yes | `POST` selects POST; every other value currently falls back to GET |
| `user` | No | Basic Authentication username for POST requests |
| `token` | No | Basic Authentication token/password for POST requests |

### Jenkins settings

| Field | Required | Description |
|---|---:|---|
| `user` | Yes | Jenkins API username |
| `token` | Yes | Jenkins API token |
| `url_format` | Yes | Job URL containing `{service-name}` and `{env}` placeholders |

## Validation and CI

Run the same basic checks used by the repository workflows:

~~~bash
go build ./...
go test ./...
go vet ./...
~~~

GitHub Actions currently:

- builds and tests the Go project on pushes and pull requests targeting `master`
- verifies that the Docker image builds successfully

## Failure handling

Current behavior:

- malformed request bodies return HTTP `400`
- downstream responses in the `2xx` range are treated as successful
- downstream connection errors and non-`2xx` responses are logged
- Slack receives a simple success or failure response
- unknown commands are rejected
- bot-generated messages are ignored to prevent response loops

Current limitations:

- no downstream request timeout
- no retries or exponential backoff
- no asynchronous job queue
- no idempotency protection
- no health or readiness endpoints
- no metrics, tracing, or structured audit events
- Jenkins acceptance is reported, but the bot does not track the final build result

## Security and production readiness

The current code is suitable for learning and controlled internal testing, but it should not be exposed to the public internet as-is.

Before production use, implement the following controls:

1. **Verify Slack signatures.** Validate `X-Slack-Signature` and `X-Slack-Request-Timestamp` using the Slack signing secret before parsing or executing an event.
2. **Authorize every command.** Allowlist approved Slack workspaces, channels, users, services, and environments. Require additional approval for production deployments.
3. **Keep secrets out of files and images.** Load credentials from environment variables or a secrets manager, rotate them regularly, and never commit a real `config.json`.
4. **Validate user input.** Restrict `service-name` and `env` to known values before inserting them into a Jenkins URL.
5. **Add safe networking behavior.** Configure connection and request timeouts, bounded retries, TLS validation, response-size limits, and egress restrictions.
6. **Protect against duplicate events.** Record Slack event IDs and make commands idempotent where possible.
7. **Create an audit trail.** Record who requested an action, what target was selected, when it ran, and its final outcome without logging secrets.
8. **Use least privilege.** Give the Slack bot, Jenkins account, and downstream API credentials only the permissions required for their tasks.
9. **Separate environments.** Use different credentials and policies for development, staging, and production.
10. **Add operational controls.** Provide health checks, metrics, alerting, rate limits, graceful shutdown, and final Jenkins build-status tracking.

For production deployment commands, a safer flow is:

~~~text
Slack request -> identity and policy check -> approval -> queued execution
              -> Jenkins/API -> status tracking -> audited Slack response
~~~

## Project structure

| Path | Purpose |
|---|---|
| `main.go` | Slack event handler, command routing, API calls, and Jenkins trigger |
| `config.json_template` | Example local configuration |
| `Dockerfile` | Multi-stage container build |
| `.github/workflows/go.yml` | Go build and test workflow |
| `.github/workflows/docker-image.yml` | Docker build validation |
| `SECURITY.md` | Security reporting policy |

## Background

This project was created to automate repetitive operational tasks from Slack and is described in more detail in:

[How to create a bot to automate daily tasks using Slack](https://www.0937686468.com/2024/09/how-to-set-up-bot-to-automate-daily.html)

## Author

**Nhàn Nguyễn**

- GitHub: [@nhannguyensy](https://github.com/nhannguyensy)
- Website: [0937686468.com](https://www.0937686468.com/)
