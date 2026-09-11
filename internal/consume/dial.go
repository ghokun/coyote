package consume

import (
	"crypto/tls"

	failed "github.com/ghokun/coyote/error"
	amqp "github.com/rabbitmq/amqp091-go"
)

func dial(rawURL string, insecure bool) (*amqp.Connection, error) {
	conn, err := amqp.DialTLS(rawURL, &tls.Config{InsecureSkipVerify: insecure})
	if err != nil {
		return nil, failed.Because("failed to connect to broker:", err)
	}
	return conn, nil
}

// DialURL is the public dial used by daemon helpers (rm --delete-queue).
func DialURL(rawURL string, insecure bool) (conn *amqp.Connection, err error) {
	return dial(rawURL, insecure)
}

// DeleteQueue removes a persistent interceptor queue. Best-effort helper for
// `rm --delete-queue`; returns an error when the broker is unreachable.
func DeleteQueue(rawURL string, insecure bool, queue string) error {
	conn, err := dial(rawURL, insecure)
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return failed.Because("failed to open a channel:", err)
	}
	defer ch.Close()
	if _, err := ch.QueueDelete(queue, false, false, false); err != nil {
		return failed.Because("failed to delete interceptor queue:", err)
	}
	return nil
}
