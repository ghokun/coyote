package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	connectionTimeout = 5 * time.Second
	heartbeat         = 10 * time.Second

	reconnectDelay    = 2 * time.Second
	maxReconnectDelay = 1 * time.Minute

	reInitDelay    = 2 * time.Second
	maxReInitDelay = 1 * time.Minute
)

var (
	errNotConnected  = errors.New("not connected to a server")
	errAlreadyClosed = errors.New("already closed: not connected to the server")
)

type Listen struct {
	c []combination
}

type combination struct {
	exchange   string
	routingKey string
}

type RabbitMQCConsumer struct {
	conn         *amqp.Connection
	channel      *amqp.Channel
	rabbitUrl    *url.URL
	insecure     bool
	queue        string
	persistent   bool
	deliverables *Listen

	m               *sync.Mutex
	done            chan bool
	isReady         bool
	notifyConnClose chan *amqp.Error
	notifyChanClose chan *amqp.Error
	notifyConfirm   chan amqp.Confirmation
}

type connectionError struct {
	msg   string
	fatal bool
}

func (e *connectionError) Error() string {
	return e.msg
}

func (l *Listen) String() string {
	return ""
}

func (l *Listen) Set(value string) (err error) {
	for _, comb := range strings.Split(value, ",") {
		pair := strings.Split(comb, "=")
		length := len(pair)
		if length == 1 {
			if len(pair[0]) < 1 {
				return fmt.Errorf("exchange name can not be empty")
			}
			l.c = append(l.c, combination{exchange: pair[0], routingKey: "#"})
		} else if length == 2 {
			if len(pair[0]) < 1 {
				return fmt.Errorf("exchange name can not be empty")
			}
			if len(pair[1]) < 1 {
				return fmt.Errorf("routing key can not be empty when '=' is provided")
			}
			l.c = append(l.c, combination{exchange: pair[0], routingKey: pair[1]})
		} else {
			return fmt.Errorf("valid values are ['a=x' 'a,b' 'a=x,b=y' 'a,b=y'] where a and b are exchanges, x and y are routing keys")
		}
	}
	return nil
}

func NewRabbitMQClient(rabbitUrl *url.URL, queue string, insecure, persistent bool, deliverables *Listen) *RabbitMQCConsumer {
	client := RabbitMQCConsumer{
		rabbitUrl:    rabbitUrl,
		insecure:     insecure,
		queue:        queue,
		persistent:   persistent,
		deliverables: deliverables,
		done:         make(chan bool),
		m:            &sync.Mutex{},
	}
	go client.handleReconnect()
	return &client
}

func (client *RabbitMQCConsumer) handleReconnect() {
	reconnectDelay := reconnectDelay
	for {
		client.m.Lock()
		client.isReady = false
		client.m.Unlock()

		log.Println("Attempting to connect")

		conn, err := client.connect()
		if err != nil {

			if err.fatal {
				log.Printf("Fatal error connecting to RabbitMQ: %v", err)
				client.isReady = false
				close(client.done)
				return
			}

			log.Printf("Failed to connect. Retrying...")
			select {
			case <-client.done:
				return
			case <-time.After(reconnectDelay):
				reconnectDelay = min(maxReconnectDelay, reconnectDelay*2)
			}
			continue
		}

		if done := client.handleReInit(conn); done {
			break
		}
	}
}

func (client *RabbitMQCConsumer) connect() (*amqp.Connection, *connectionError) {
	conn, err := amqp.DialConfig(client.rabbitUrl.String(), amqp.Config{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: client.insecure},
		Locale:          "en_US",
		Dial:            amqp.DefaultDial(connectionTimeout),
		Heartbeat:       heartbeat,
	})
	if err != nil {
		var e *amqp.Error
		switch {
		case errors.As(err, &e):
			if e.Code == amqp.AccessRefused {
				return nil, &connectionError{
					msg: fmt.Sprintf("%s %v", color.RedString("access denied"), err), fatal: true,
				}
			} else {
				return nil, &connectionError{
					msg: fmt.Sprintf("%s %v", color.RedString("failed to connect to RabbitMQ:"), err),
				}
			}
		default:
			return nil, &connectionError{
				msg: fmt.Sprintf("%s %v", color.RedString("failed to connect to RabbitMQ:"), err),
			}
		}
	}

	client.changeConnection(conn)
	log.Println("Connected")
	return conn, nil
}

func (client *RabbitMQCConsumer) handleReInit(conn *amqp.Connection) bool {
	reInitDelay := reInitDelay
	for {
		client.m.Lock()
		client.isReady = false
		client.m.Unlock()

		err := client.init(conn)
		if err != nil {
			log.Println("Failed to initialize channel, retrying...")

			select {
			case <-client.done:
				return true
			case <-client.notifyConnClose:
				log.Println("Connection closed, reconnecting...")
				return false
			case <-time.After(reInitDelay):
				reInitDelay = min(maxReInitDelay, reInitDelay*2)
			}
			continue
		}

		select {
		case <-client.done:
			return true
		case <-client.notifyConnClose:
			log.Println("Connection closed, reconnecting...")
			return false
		case <-client.notifyChanClose:
			log.Println("Channel closed, re-running init...")
		}
	}
}

func (client *RabbitMQCConsumer) init(conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}

	err = ch.Confirm(false)
	if err != nil {
		return err
	}
	q, err := ch.QueueDeclare(
		client.queue,
		false,              // is durable
		!client.persistent, // is auto delete
		!client.persistent, // is exclusive
		false,              // is no wait
		nil,                // args
	)
	if err != nil {
		return err
	}

	for _, c := range client.deliverables.c {
		err = ch.ExchangeDeclarePassive(
			c.exchange, // exchange name
			"topic",    // exchange kind
			true,       // is durable
			false,      // is auto delete
			false,      // is internal
			false,      // is no wait
			nil,        // args
		)
		if err != nil {
			return err
		}

		err = ch.QueueBind(
			q.Name,       // interceptor queue name
			c.routingKey, // routing key to bind
			c.exchange,   // exchange to listen
			false,        // is no wait
			nil,          // args
		)
		if err != nil {
			return err
		} else {
			log.Printf("👂 Listening from exchange %s with routing key %s", color.YellowString(c.exchange), color.YellowString(c.routingKey))
		}
	}

	client.changeChannel(ch)
	client.m.Lock()
	client.isReady = true
	client.m.Unlock()
	log.Println("Client init done")

	return nil
}

func (client *RabbitMQCConsumer) changeConnection(connection *amqp.Connection) {
	client.conn = connection
	client.notifyConnClose = make(chan *amqp.Error, 1)
	client.conn.NotifyClose(client.notifyConnClose)
}

func (client *RabbitMQCConsumer) changeChannel(channel *amqp.Channel) {
	client.channel = channel
	client.notifyChanClose = make(chan *amqp.Error, 1)
	client.notifyConfirm = make(chan amqp.Confirmation, 1)
	client.channel.NotifyClose(client.notifyChanClose)
	client.channel.NotifyPublish(client.notifyConfirm)
}

func (client *RabbitMQCConsumer) Consume() (<-chan amqp.Delivery, error) {
	for {
		client.m.Lock()
		if !client.isReady {
			client.m.Unlock()
			select {
			case <-time.After(time.Second):
			case <-client.done:
				return nil, errNotConnected
			}
		} else {
			client.m.Unlock()
			break
		}
	}

	if err := client.channel.Qos(
		1,     // prefetchCount
		0,     // prefetchSize
		false, // global
	); err != nil {
		return nil, err
	}

	return client.channel.Consume(
		client.queue,
		"",    // Consumer
		true,  // Auto-Ack
		false, // Exclusive
		false, // No-local
		false, // No-Wait
		nil,   // Args
	)
}

func (client *RabbitMQCConsumer) Close() error {
	if client.persistent {
		var exchanges []string
		for _, comb := range client.deliverables.c {
			exchanges = append(exchanges, comb.exchange)
		}
		log.Printf("⚠️ Please do not forget to clean up the persistent interceptor queue %s manually in the following exhanges: %s",
			color.YellowString(client.queue),
			color.YellowString(strings.Join(exchanges, ", ")))
	}

	client.m.Lock()
	defer client.m.Unlock()

	if !client.isReady {
		return errAlreadyClosed
	}
	close(client.done)
	err := client.channel.Close()
	if err != nil {
		return err
	}
	err = client.conn.Close()
	if err != nil {
		return err
	}

	client.isReady = false
	return nil
}
