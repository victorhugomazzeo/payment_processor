package payment

type Status string

const (
	StatusCreated    Status = "created"
	StatusAuthorized Status = "authorized"
	StatusCaptured   Status = "captured"
	StatusSettled    Status = "settled"
	StatusDenied     Status = "denied"
	StatusVoided     Status = "voided"
	StatusAbandoned  Status = "abandoned"
	StatusRefunded   Status = "refunded"
	StatusDisputed   Status = "disputed"
)
