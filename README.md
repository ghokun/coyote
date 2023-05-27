# Coyote

Coyote is a RabbitMQ message sink. The default routing key is `#` so every message in the given `exchange` is routed to a ephemeral `interceptor` queue.

## Install

```shell
brew install ghokun/tap/coyote
```

## Usage

```shell
NAME:
   coyote - Coyote is a RabbitMQ message sink.

USAGE:
   coyote [global options] command [command options] [arguments...]

VERSION:
   development

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --url value       RabbitMQ url, must start with amqps:// or amqp://.
   --exchange value  Exchange name to listen messages.
   --type value      Exchange type. Valid values are direct, fanout, topic and headers. (default: "topic")
   --bind value      Routing key to bind. Binds to all queues if not provided. (default: "#")
   --queue value     Interceptor queue name. (default: "interceptor")
   --insecure        Skips certificate verification. (default: false)
   --noprompt        Disables password prompt. (default: false)
   --nopassive       Declares queue actively. (default: false)
   --help, -h        show help
   --version, -v     print the version

# Example
coyote --url amqp://guest:guest@localhost --exchange your_exchange
```
