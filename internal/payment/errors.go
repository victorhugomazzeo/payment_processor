package payment

import "errors"

var ErrMerchantNotFound = errors.New("merchant not found")

const fkViolationCode = "23503"
