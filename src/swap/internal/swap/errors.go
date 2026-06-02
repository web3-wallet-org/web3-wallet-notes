package swap

import (
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrNotFound         = errors.New("not found")
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrUnsupportedChain = errors.New("unsupported chain")
	ErrQuoteExpired     = errors.New("quote expired")
	ErrRiskBlocked      = errors.New("risk blocked")
	ErrConflict         = errors.New("conflict")
	ErrProviderDisabled = errors.New("provider disabled")
)

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e APIError) Error() string {
	return e.Message
}

func apiError(err error) APIError {
	switch {
	case errors.Is(err, ErrInvalidArgument):
		return APIError{Code: "INVALID_ARGUMENT", Message: err.Error(), HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrUnsupportedChain):
		return APIError{Code: "UNSUPPORTED_CHAIN", Message: err.Error(), HTTPStatus: http.StatusBadRequest}
	case errors.Is(err, ErrQuoteExpired):
		return APIError{Code: "QUOTE_EXPIRED", Message: err.Error(), HTTPStatus: http.StatusConflict}
	case errors.Is(err, ErrRiskBlocked):
		return APIError{Code: "RISK_BLOCKED", Message: err.Error(), HTTPStatus: http.StatusForbidden}
	case errors.Is(err, ErrConflict):
		return APIError{Code: "CONFLICT", Message: err.Error(), HTTPStatus: http.StatusConflict}
	case errors.Is(err, ErrNotFound):
		return APIError{Code: "NOT_FOUND", Message: err.Error(), HTTPStatus: http.StatusNotFound}
	default:
		return APIError{Code: "INTERNAL_ERROR", Message: "internal error", HTTPStatus: http.StatusInternalServerError}
	}
}

func invalidArgument(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, fmt.Sprintf(format, args...))
}
