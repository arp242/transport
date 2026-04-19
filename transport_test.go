package transport

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

func errorContains(have error, want string) bool {
	if have == nil {
		return want == ""
	}
	if want == "" {
		return false
	}
	return strings.Contains(have.Error(), want)
}

type statusErr int

func (s statusErr) Error() string { return fmt.Sprintf("HTTP status %d", s) }

func mustGet(c *http.Client, u string) (string, error) {
	resp, err := c.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		err = statusErr(resp.StatusCode)
	}
	return string(b), err
}
