package common

import (
	"encoding/json"
	"fmt"
	"io"
)

// SendOSC sends an OSC 31337 sequence with a JSON payload
func SendOSC(w io.Writer, action string, params map[string]interface{}) error {
	payload := make(map[string]interface{})
	payload["action"] = action
	for k, v := range params {
		payload[k] = v
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "\x1b]31337;%s\x07", string(jsonData))
	return err
}
