package responses

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type museArgumentsState struct{ values map[string]*strings.Builder }

const (
	maxMuseNumberDigits       = 4096
	maxMuseArgumentsExpansion = 64 * 1024
)

type museNumberBudget struct {
	remaining int
	exhausted bool
}

// Meta's item.done can omit arguments already supplied by arguments.done.
// Preserve that completed value instead of replacing a valid call with {}.
func restoreMuseArguments(payload []byte, model string, param *any) []byte {
	if !strings.HasPrefix(model, "muse-spark-") || param == nil {
		return payload
	}
	state, ok := (*param).(*museArgumentsState)
	if !ok {
		state = &museArgumentsState{values: map[string]*strings.Builder{}}
		*param = state
	}
	kind := gjson.GetBytes(payload, "type").String()
	id := gjson.GetBytes(payload, "item_id").String()
	value := state.values[id]
	switch kind {
	case "response.function_call_arguments.delta":
		if value == nil {
			value = &strings.Builder{}
			state.values[id] = value
		}
		value.WriteString(gjson.GetBytes(payload, "delta").String())
	case "response.function_call_arguments.done":
		if gjson.GetBytes(payload, "arguments").String() == "" && value != nil && value.Len() > 0 {
			payload, _ = sjson.SetBytes(payload, "arguments", value.String())
		}
		payload = normalizeMuseToolArguments(payload, model)
		completed := &strings.Builder{}
		completed.WriteString(gjson.GetBytes(payload, "arguments").String())
		state.values[id] = completed
	case "response.output_item.done":
		if gjson.GetBytes(payload, "item.type").String() == "function_call" {
			id = gjson.GetBytes(payload, "item.id").String()
			value = state.values[id]
			if gjson.GetBytes(payload, "item.arguments").String() == "" && value != nil && value.Len() > 0 {
				payload, _ = sjson.SetBytes(payload, "item.arguments", value.String())
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
	budget := &museNumberBudget{remaining: maxMuseArgumentsExpansion}
	decoded = normalizeMuseNumbers(decoded, budget)
	if budget.exhausted {
		// Keep the entire original argument string rather than returning a
		// partially normalized object whose fields depend on map iteration order.
		return payload
	}
	normalized, err := json.Marshal(decoded)
	if err != nil {
		return payload
	}
	updated, err := sjson.SetBytes(payload, path, string(normalized))
	if err != nil {
		return payload
	}
	return updated
}

func normalizeMuseNumbers(value any, budget *museNumberBudget) any {
	if budget.exhausted {
		return value
	}
	switch v := value.(type) {
	case json.Number:
		return normalizeMuseNumber(v, budget)
	case []any:
		for i, x := range v {
			v[i] = normalizeMuseNumbers(x, budget)
		}
	case map[string]any:
		for k, x := range v {
			v[k] = normalizeMuseNumbers(x, budget)
		}
	}
	return value
}

// Normalize lexically so a compact exponent can never cause an unbounded
// big-integer allocation. Input has already passed JSON validation. Check both
// the individual number and the shared argument budget before adding zeroes.
func normalizeMuseNumber(value json.Number, budget *museNumberBudget) json.Number {
	raw := string(value)
	if !strings.ContainsAny(raw, ".eE") {
		return value
	}
	number := raw
	sign := ""
	if strings.HasPrefix(number, "-") {
		sign, number = "-", number[1:]
	}
	exponent := 0
	if pos := strings.IndexAny(number, "eE"); pos >= 0 {
		parsed, err := strconv.ParseInt(number[pos+1:], 10, 32)
		if err != nil || parsed > maxMuseNumberDigits || parsed < -maxMuseNumberDigits {
			return value
		}
		exponent, number = int(parsed), number[:pos]
	}
	if pos := strings.IndexByte(number, '.'); pos >= 0 {
		exponent -= len(number) - pos - 1
		number = number[:pos] + number[pos+1:]
	}
	number = strings.TrimLeft(number, "0")
	if number == "" {
		return json.Number("0")
	}
	if exponent < 0 {
		if exponent < -len(number) {
			return value
		}
		end := len(number) + exponent
		if strings.Trim(number[end:], "0") != "" {
			return value
		}
		number, exponent = number[:end], 0
	}
	if len(number) > maxMuseNumberDigits-exponent {
		return value
	}
	growth := len(sign) + len(number) + exponent - len(raw)
	if growth > budget.remaining {
		budget.exhausted = true
		return value
	}
	if growth > 0 {
		budget.remaining -= growth
	}
	return json.Number(sign + number + strings.Repeat("0", exponent))
}
