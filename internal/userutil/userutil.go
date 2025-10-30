package userutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

var (
	disallowedCharacter      = regexp.MustCompile(`[^\w\-.]`)
	consecutivePeriodsDashes = regexp.MustCompile(`[\-.]{2,}`)
	sequencesToTrim          = regexp.MustCompile(`(^[\-.])|(\.$)|`)
)

// NormalizeIdentifier normalizes a proposed identifier into a desired format:
//   - Any characters not in `[a-zA-Z0-9-._]` are replaced with `-`
//   - Usernames with exactly one `@` character are interpreted as an email address, so the username will be extracted by truncating at the `@` character.
//   - Usernames with two or more `@` characters are not considered an email address, so the `@` will be treated as a non-standard character and be replaced with `-`
//   - Usernames with consecutive `-` or `.` characters are not allowed, so they are replaced with a single `-` or `.`
//   - Usernames that start with `.` or `-` are not allowed, starting periods and dashes are removed
//   - Usernames that end with `.` are not allowed, ending periods are removed
//
// Usernames that could not be converted return an error.
//
// Copied from https://github.com/sourcegraph/sourcegraph/blob/73046a7be42a00c403cbbe7b329fccedb057fe56/cmd/frontend/auth/auth.go#L75
func NormalizeIdentifier(id string) (string, error) {
	origName := id

	// If the username is an email address, extract the username part.
	if i := strings.Index(id, "@"); i != -1 && i == strings.LastIndex(id, "@") {
		id = id[:i]
	}

	// Replace all non-alphanumeric characters with a dash.
	id = disallowedCharacter.ReplaceAllString(id, "-")

	// Replace all consecutive dashes and periods with a single dash.
	id = consecutivePeriodsDashes.ReplaceAllString(id, "-")

	// Trim leading and trailing dashes and periods.
	id = sequencesToTrim.ReplaceAllString(id, "")

	id = strings.ToLower(id)

	if id == "" {
		return "", errors.Errorf("username %q could not be normalized to acceptable format", origName)
	}
	return id, nil
}

// SendWebhook sends a webhook to the configured webhook URL with the given content as json.
func SendWebhook(data map[string]any, endpoint string) (err error) {
	// Encode data to JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	// Set up the HTTP request

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonData))
	if err != nil {
		return
	}
	// Set the content type header to JSON
	req.Header.Set("Content-Type", "application/json")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}

	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return errors.Errorf("webhook failed with status code %d", resp.StatusCode)
	}
	return nil

}
