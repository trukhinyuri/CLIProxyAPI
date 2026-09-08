package responses

import (
	"encoding/json"
	"math/big"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type museArgumentsState struct{ values map[string]string }

// Meta's item.done can omit arguments already supplied by arguments.done.
// Preserve that completed value instead of replacing a valid call with {}.
func restoreMuseArguments(payload []byte, model string, param *any) []byte {
	if !strings.HasPrefix(model, "muse-spark-") || param == nil {
		return payload
	}
	state, ok := (*param).(*museArgumentsState)
	if !ok {
		state = &museArgumentsState{values: map[string]string{}}
		*param = state
	}
	kind := gjson.GetBytes(payload, "type").String()
	id := gjson.GetBytes(payload, "item_id").String()
	switch kind {
	case "response.function_call_arguments.delta":
		state.values[id] += gjson.GetBytes(payload, "delta").String()
	case "response.function_call_arguments.done":
		if gjson.GetBytes(payload, "arguments").String() == "" && state.values[id] != "" {
			payload, _ = sjson.SetBytes(payload, "arguments", state.values[id])
		}
		payload = normalizeMuseToolArguments(payload, model)
		state.values[id] = gjson.GetBytes(payload, "arguments").String()
	case "response.output_item.done":
		if gjson.GetBytes(payload, "item.type").String() == "function_call" {
			id = gjson.GetBytes(payload, "item.id").String()
			if gjson.GetBytes(payload, "item.arguments").String() == "" && state.values[id] != "" {
				payload, _ = sjson.SetBytes(payload, "item.arguments", state.values[id])
			}
			payload = normalizeMuseToolArguments(payload, model)
			delete(state.values, id)
		}
	}
	return payload
}

// Meta can emit integral JSON numbers as 120000.0 and empty arguments for
// parameterless calls. Codex's integer tool fields require canonical integers.
// Normalize only completed Muse call items; never alter strings or fractions.
func normalizeMuseToolArguments(payload []byte, model string) []byte {
	if !strings.HasPrefix(model, "muse-spark-") {
		return payload
	}
	path := ""
	switch gjson.GetBytes(payload, "type").String() {
	case "response.output_item.done":
		if gjson.GetBytes(payload, "item.type").String() == "function_call" {
			path = "item.arguments"
		}
	case "response.function_call_arguments.done":
		path = "arguments"
	}
	if path == "" {
		return payload
	}
	value := gjson.GetBytes(payload, path)
	if !value.Exists() || value.Type != gjson.String {
		return payload
	}
	raw := value.String()
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		return payload
	}
	var decoded any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&decoded) != nil {
		return payload
	}
	normalized, err := json.Marshal(normalizeMuseNumbers(decoded))
	if err != nil {
		return payload
	}
	updated, err := sjson.SetBytes(payload, path, string(normalized))
	if err != nil {
		return payload
	}
	return updated
}

func normalizeMuseNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		if n, ok := new(big.Rat).SetString(string(v)); ok && n.IsInt() {
			return json.Number(n.Num().String())
		}
	case []any:
		for i, x := range v {
			v[i] = normalizeMuseNumbers(x)
		}
	case map[string]any:
		for k, x := range v {
			v[k] = normalizeMuseNumbers(x)
		}
	}
	return value
}
