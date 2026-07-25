package payment

type EventType string

const (
	EventTypePaymentCreated    EventType = "payment_created"
	EventTypePaymentAuthorized EventType = "payment_authorized"
	EventTypePaymentDenied     EventType = "payment_denied"
	EventTypePaymentError      EventType = "payment_error"
)
