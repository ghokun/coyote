# Coyote

Coyote is a RabbitMQ message sink.

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
   --queue value     Interceptor queue name. (default: "interceptor")
   --bind value      Routing key to bind. (default: "#")
   --help, -h        show help
   --version, -v     print the version

# Example
coyote --url amqp://guest:guest@localhost --exchange your_exchange
```
