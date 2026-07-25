package payment

type EventProcessedDetails struct {
	ReturnCode    string `json:"return_code"`
	ReturnMessage string `json:"return_message"`
}

type EventErrorDetails struct {
	Error string `json:"error"`
}
