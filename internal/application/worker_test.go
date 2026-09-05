package application

import (
	"context"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestDeliveryWorkerMarksSuccess(t *testing.T) {
	outbox := &fakeOutbox{deliveries: []domain.Delivery{{Key: "delivery-1", Notification: domain.Notification{EventKey: "event-1"}}}}
	messenger := &fakeMessenger{}
	worker := NewDeliveryWorker(outbox, messenger, DeliveryWorkerConfig{BatchSize: 1}, nil)

	if err := worker.process(context.Background()); err != nil {
		t.Fatalf("process() error = %v", err)
	}
	if len(messenger.notifications) != 1 || outbox.delivered != "delivery-1" || outbox.failed != nil {
		t.Fatalf("messenger=%#v outbox=%#v", messenger, outbox)
	}
}

func TestDeliveryWorkerClassifiesMessengerFailure(t *testing.T) {
	outbox := &fakeOutbox{deliveries: []domain.Delivery{{Key: "delivery-1", Notification: domain.Notification{EventKey: "event-1"}}}}
	messenger := &fakeMessenger{err: &classifiedTestError{failure: domain.DeliveryFailure{Class: "rate_limit", Retryable: true, Message: "slow down"}}}
	worker := NewDeliveryWorker(outbox, messenger, DeliveryWorkerConfig{BatchSize: 1}, nil)

	if err := worker.process(context.Background()); err != nil {
		t.Fatalf("process() error = %v", err)
	}
	if outbox.failed == nil || outbox.failed.Class != "rate_limit" || !outbox.failed.Retryable {
		t.Fatalf("failure=%#v", outbox.failed)
	}
}

type fakeOutbox struct {
	deliveries []domain.Delivery
	delivered  string
	failed     *domain.DeliveryFailure
}

func (o *fakeOutbox) Claim(context.Context, string, int) ([]domain.Delivery, error) {
	deliveries := o.deliveries
	o.deliveries = nil
	return deliveries, nil
}

func (o *fakeOutbox) MarkDelivered(_ context.Context, key string, _ domain.DeliveryReceipt) error {
	o.delivered = key
	return nil
}

func (o *fakeOutbox) MarkFailed(_ context.Context, _ string, failure domain.DeliveryFailure) error {
	o.failed = &failure
	return nil
}

type fakeMessenger struct {
	notifications []domain.Notification
	err           error
}

func (m *fakeMessenger) Send(_ context.Context, notification domain.Notification) (domain.DeliveryReceipt, error) {
	if m.err != nil {
		return domain.DeliveryReceipt{}, m.err
	}
	m.notifications = append(m.notifications, notification)
	return domain.DeliveryReceipt{ProviderMessageID: "message-1"}, nil
}

type classifiedTestError struct {
	failure domain.DeliveryFailure
}

func (e *classifiedTestError) Error() string {
	return e.failure.Message
}

func (e *classifiedTestError) DeliveryFailure() domain.DeliveryFailure {
	return e.failure
}
