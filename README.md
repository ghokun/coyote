# Coyote

Coyote is a RabbitMQ message sink.

## Install

```shell
brew install ghokun/tap/coyote
```

## Usage

```shell
Usage of coyote:
  -bind string
        Routing key to bind. (default "#")
  -exchange string
        Exchange name to listen messages.
  -queue string
        Interceptor queue name. (default "interceptor")
  -url string
        RabbitMQ url, must start with amqps:// or amqp://.

# Example
coyote -url amqp://guest:guest@localhost -exchange your_exchange
```
