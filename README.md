# Coyote

Coyote is a RabbitMQ message sink. The default routing key is `#` so every message in the given `exchange` is routed to an `interceptor` queue.

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
   # Store all messages from 'myexchange' into 'events.sqlite' file, prompting for password
   coyote --url amqps://user@myurl --exchange myexchange=# --store events.sqlite

   # Store all messages with routing key 'mykey' from 'myexchange' into events.sqlite file without prompting for password
   coyote --url amqps://user:password@myurl --noprompt --exchange myexchange=mykey --store events.sqlite

   # Capture all messages from 'myexchange' without certificate verification
   coyote --url amqps://user:password@myurl --noprompt --insecure --exchange myexchange=#

   Exchange binding formats:
    --exchange myexchange=#                          # All messages in single exchange
    --exchange myexchange1=mykey1                    # Messages with routing key in a single exchange
    --exchange myexchange1=mykey1,myexchange1=mykey2 # Messages with routing keys in a single exchange
    --exchange myexchange1=#,myexchange2=#           # All messages in multiple exchanges
    --exchange myexchange1=mykey1,myexchange2=mykey2 # Messages with routing keys in multiple exchanges
    --exchange myexchange1=#,myexchange2=mykey2      # Messages with or without specific routing keys in multiple exchanges

VERSION:
   development

GLOBAL OPTIONS:
   --url string                                           RabbitMQ url, must start with amqps:// or amqp://.
   --exchange string=string [ --exchange string=string ]  Exchange & routing key combinations to listen messages.
   --queue string                                         Interceptor queue name. If provided, interceptor queue will not be auto deleted.
   --store string                                         SQLite filename to store events.
   --insecure                                             Skips certificate verification. (default: false)
   --noprompt                                             Disables password prompt. (default: false)
   --silent                                               Disables terminal print. (default: false)
   --help, -h                                             show help
   --version, -v                                          print the version
```
