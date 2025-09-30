# Coyote

Coyote is a RabbitMQ message sink. Creates an interceptor queue for the given exchange routing key pairs and captures messages.

Features:

- Basic and OAuth2.0 authentication
- Store captured messages into SQLite database
- Capture messages from multiple exchanges and routing keys
- Create ephemeral or persistent queues

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
   --oauth                                                Use OAuth 2.0 for authentication. (default: false)
   --redirect-url string                                  OIDC callback url for OAuth 2.0
   --insecure                                             Skips certificate verification. (default: false)
   --exchange string=string [ --exchange string=string ]  Exchange & routing key combinations to listen messages.
   --queue string                                         Interceptor queue name. If provided, interceptor queue will not be auto deleted.
   --store string                                         SQLite filename to store events.
   --silent                                               Disables terminal print. (default: false)
   --help, -h                                             show help
   --version, -v                                          print the version
```
