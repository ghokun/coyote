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
   coyote [global options]

   Examples:
   coyote --url amqps://user@myurl --exchange myexchange --store events.sqlite
   coyote --url amqps://user:password@myurl --noprompt --exchange myexchange --store events.sqlite
   coyote --url amqps://user:password@myurl --noprompt --insecure --exchange myexchange

   Exchange binding formats:
    --exchange myexchange                            # All messages in single exchange
    --exchange myexchange1=mykey1                    # Messages with routing key in a single exchange
    --exchange myexchange1=mykey1,myexchange1=mykey2 # Messages with routing keys in a single exchange
    --exchange myexchange1,myexchange2               # All messages in multiple exchanges
    --exchange myexchange1=mykey1,myexchange2=mykey2 # Messages with routing keys in multiple exchanges
    --exchange myexchange1,myexchange2=mykey2        # Messages with or without routing keys in multiple exchanges

VERSION:
   v0.10.0

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --url value       RabbitMQ url, must start with amqps:// or amqp://.
   --exchange value  Exchange & routing key combinations to listen messages.
   --queue value     Interceptor queue name. (default: "interceptor")
   --store value     SQLite filename to store events.
   --insecure        Skips certificate verification (default: false)
   --noprompt        Disables password prompt (default: false)
   --help, -h        show help
   --version, -v     print the version
```
