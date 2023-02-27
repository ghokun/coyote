package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func fatalOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("💥 %s: %s", msg, err)
	}
}

func isFlagSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func main() {
	url := flag.String("url", "", "RabbitMQ url, must start with amqps:// or amqp://.")
	exchange := flag.String("exchange", "", "Exchange name to listen messages.")
	queue := flag.String("queue", "interceptor", "Interceptor queue name.")
	routingKey := flag.String("bind", "#", "Routing key to bind.")
	flag.Parse()

	if !isFlagSet("url") || !isFlagSet("exchange") {
		flag.Usage()
		os.Exit(1)
	}

	conn, err := amqp.Dial(*url)
	fatalOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	fatalOnError(err, "Failed to open a channel")
	defer ch.Close()

	err = ch.ExchangeDeclarePassive(*exchange, "topic", false, true, false, false, nil)
	fatalOnError(err, "Failed to connect to exchange")

	q, err := ch.QueueDeclare(fmt.Sprintf("%s.%s", *queue, uuid.NewString()), false, true, false, false, nil)
	fatalOnError(err, "Failed to declare a queue")
	ch.QueueBind(q.Name, *routingKey, *exchange, false, nil)

	msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	fatalOnError(err, "Failed to register a consumer")

	var forever chan struct{}
	go func() {
		for d := range msgs {
			log.Printf("📧 Received a message on queue %s: %s", d.RoutingKey, d.Body)
		}
	}()

	log.Printf("⏳ Waiting for messages. To exit press CTRL+C")
	<-forever
}
