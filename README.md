# Coyote

Coyote is a RabbitMQ message sink. Creates an interceptor queue for the given exchange routing key pairs and captures messages.

Features:

- Basic and OAuth2.0 authentication
- Background daemon + CLI (docker-like): `consume` auto-starts the daemon
- Manage multiple consume tasks: `list`, `stop`, `pause`, `resume`, `rm`
- Fetch stored messages and inspect task logs through the daemon
- Store captured messages into SQLite database
- Capture messages from multiple exchanges and routing keys
- Create ephemeral or persistent queues

## Install

```shell
brew install ghokun/tap/coyote
```

## Usage

```shell
coyote consume --url amqps://user@myurl --exchange myexchange=# --store events.sqlite
coyote list
coyote logs <task-id> --tail 100
coyote fetch <task-id> --limit 100
```

```shell
NAME:
   coyote - Coyote is a RabbitMQ message sink.

USAGE:
   coyote [global options] [command [command options]]

VERSION:
   development

DESCRIPTION:
   Coyote is a RabbitMQ message sink.

   Creates an interceptor queue for the given exchange/routing-key pairs and
   captures messages. The same binary acts as CLI and background daemon:
   'consume' auto-starts the daemon when needed.

   Exchange binding formats:
    --exchange myexchange=#                          # All messages in single exchange
    --exchange myexchange1=mykey1                    # Messages with routing key in a single exchange
    --exchange myexchange1=mykey1,myexchange1=mykey2 # Messages with routing keys in a single exchange
    --exchange myexchange1=#,myexchange2=#           # All messages in multiple exchanges
    --exchange myexchange1=mykey1,myexchange2=mykey2 # Messages with routing keys in multiple exchanges
    --exchange myexchange1=#,myexchange2=mykey2      # Messages with or without specific routing keys in multiple exchanges

COMMANDS:
   consume          Consume messages via the daemon (auto-starts it when needed).
   list, ps         List daemon tasks.
   start            Start a stopped or failed task.
   stop             Stop a running or paused task (keeps the queue).
   pause            Pause a running task (keeps the queue).
   resume           Resume a paused task.
   rm               Stop and remove a task.
   logs             Show a task's log.
   fetch, messages  Fetch stored messages of a task through the daemon.
   help, h          Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --url string                                           RabbitMQ url, must start with amqps:// or amqp://.
   --oauth                                                Use OAuth 2.0 for authentication.
   --redirect-url string                                  OIDC callback url for OAuth 2.0
   --insecure                                             Skips certificate verification.
   --exchange string=string [ --exchange string=string ]  Exchange & routing key combinations to listen messages. (default: myexchange=#)
   --queue string                                         Interceptor queue name. If provided, interceptor queue will not be auto deleted.
   --store string                                         SQLite filename to store events.
   --silent                                               Disables terminal print.
   --help, -h                                             show help
   --version, -v                                          print the version
```
