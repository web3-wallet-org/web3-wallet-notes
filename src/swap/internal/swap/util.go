package swap

import (
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strings"
)

func normalizeAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func sameAddress(a, b string) bool {
	return normalizeAddress(a) == normalizeAddress(b)
}

func isNativeToken(address string) bool {
	return sameAddress(address, NativeTokenAddress) || address == ""
}

func containsInt64(values []int64, target int64) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:16])
}

func minAmountOut(amountOut string, slippageBps int64) (string, error) {
	out, ok := new(big.Int).SetString(amountOut, 10)
	if !ok || out.Sign() < 0 {
		return "", invalidArgument("amountOut must be unsigned integer")
	}
	if slippageBps < 0 || slippageBps > 10_000 {
		return "", invalidArgument("slippageBps must be between 0 and 10000")
	}
	numerator := big.NewInt(10_000 - slippageBps)
	result := new(big.Int).Mul(out, numerator)
	result.Div(result, big.NewInt(10_000))
	return result.String(), nil
}

func decimalStringGreaterOrEqual(left, right string) bool {
	left = decimalize(left)
	right = decimalize(right)
	l, okL := new(big.Int).SetString(left, 10)
	r, okR := new(big.Int).SetString(right, 10)
	if !okL || !okR {
		return false
	}
	return l.Cmp(r) >= 0
}

func decimalStringCmp(left, right string) int {
	left = decimalize(left)
	right = decimalize(right)
	l, okL := new(big.Int).SetString(left, 10)
	r, okR := new(big.Int).SetString(right, 10)
	if !okL || !okR {
		return 0
	}
	return l.Cmp(r)
}

func validUnsignedDecimal(value string) bool {
	if value == "" {
		return false
	}
	i, ok := new(big.Int).SetString(value, 10)
	return ok && i.Sign() >= 0
}

func decimalize(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		i, ok := new(big.Int).SetString(value[2:], 16)
		if !ok {
			return value
		}
		return i.String()
	}
	return value
}

func encodeApproveCalldata(spender, amount string) string {
	return "0x095ea7b3" + leftPadHexNoPrefix(strip0x(spender)) + leftPadDecimalNoPrefix(amount)
}

func leftPadHexNoPrefix(s string) string {
	s = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
	for len(s) < 64 {
		s = "0" + s
	}
	if len(s) > 64 {
		return s[len(s)-64:]
	}
	return s
}

func leftPadDecimalNoPrefix(value string) string {
	i, ok := new(big.Int).SetString(value, 10)
	if !ok {
		i = big.NewInt(0)
	}
	s := i.Text(16)
	for len(s) < 64 {
		s = "0" + s
	}
	return s
}
