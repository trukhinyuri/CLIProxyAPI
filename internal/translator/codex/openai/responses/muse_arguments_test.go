package responses

import (
	"bytes"
	"context"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestMuseEmptyItemRetainsCompletedArguments(t *testing.T) {
	var state any
	first := []byte(`{"type":"response.function_call_arguments.done","item_id":"fc1","arguments":"{\"timeout_ms\":120000.0,\"message\":\"keep me\"}"}`)
	restoreMuseArguments(first, "muse-spark-1.3", &state)
	final := []byte(`{"type":"response.output_item.done","item":{"id":"fc1","type":"function_call","arguments":""}}`)
	got := restoreMuseArguments(final, "muse-spark-1.3", &state)
	args := gjson.GetBytes(got, "item.arguments").String()
	if args != `{"message":"keep me","timeout_ms":120000}` {
		t.Fatalf("lost completed arguments: %s", got)
	}
}

func TestMuseCompletedArguments(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"timeout_ms":120000.0}`, `{"timeout_ms":120000}`},
		{``, `{}`},
		{`{"a":[1e3,1.5,"12.0",9007199254740993.0]}`, `{"a":[1000,1.5,"12.0",9007199254740993]}`},
	} {
		p, _ := sjson.Set(`{"type":"response.output_item.done","item":{"type":"function_call"}}`, "item.arguments", tc.in)
		got := normalizeMuseToolArguments([]byte(p), "muse-spark-1.3")
		if gjson.GetBytes(got, "item.arguments").String() != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
		if string(normalizeMuseToolArguments([]byte(p), "gpt-6-astra")) != p {
			t.Fatal("modified non-Muse response")
		}
	}
}

func TestMuseStreamingArguments(t *testing.T) {
	for _, framed := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[framed], func(t *testing.T) {
			var state any
			convert := func(event string) []byte {
				t.Helper()
				if framed {
					event = "data: " + event
				}
				out := ConvertCodexResponseToOpenAIResponses(context.Background(), "muse-spark-1.3", nil, nil, []byte(event), &state)
				if len(out) != 1 {
					t.Fatalf("expected one event, got %d", len(out))
				}
				if framed && !bytes.HasPrefix(out[0], []byte("data: ")) {
					t.Fatalf("lost SSE framing: %s", out[0])
				}
				return bytes.TrimPrefix(out[0], []byte("data: "))
			}
			convert(`{"type":"response.function_call_arguments.delta","item_id":"first","delta":"{\"timeout_ms\":"}`)
			convert(`{"type":"response.function_call_arguments.delta","item_id":"second","delta":"{\"message\":\"second\"}"}`)
			convert(`{"type":"response.function_call_arguments.delta","item_id":"first","delta":"120000.0}"}`)
			done := convert(`{"type":"response.function_call_arguments.done","item_id":"first","arguments":""}`)
			if got := gjson.GetBytes(done, "arguments").String(); got != `{"timeout_ms":120000}` {
				t.Fatalf("lost accumulated arguments: %s", done)
			}
			first := convert(`{"type":"response.output_item.done","item":{"id":"first","type":"function_call"}}`)
			if got := gjson.GetBytes(first, "item.arguments").String(); got != `{"timeout_ms":120000}` {
				t.Fatalf("lost completed arguments: %s", first)
			}
			second := convert(`{"type":"response.output_item.done","item":{"id":"second","type":"function_call","arguments":""}}`)
			if got := gjson.GetBytes(second, "item.arguments").String(); got != `{"message":"second"}` {
				t.Fatalf("mixed interleaved calls: %s", second)
			}
			if len(state.(*museArgumentsState).values) != 0 {
				t.Fatal("retained completed calls in stream state")
			}
		})
	}
}

func TestMuseExplicitItemArgumentsTakePrecedence(t *testing.T) {
	var state any
	restoreMuseArguments([]byte(`{"type":"response.function_call_arguments.done","item_id":"fc1","arguments":"{\"value\":1}"}`), "muse-spark-1.3", &state)
	got := restoreMuseArguments([]byte(`{"type":"response.output_item.done","item":{"id":"fc1","type":"function_call","arguments":"{\"value\":2.0}"}}`), "muse-spark-1.3", &state)
	if gjson.GetBytes(got, "item.arguments").String() != `{"value":2}` {
		t.Fatalf("overwrote explicit completed arguments: %s", got)
	}
}

func TestMuseArgumentsPreserveUnsupportedPayloads(t *testing.T) {
	for _, event := range []string{
		`{"type":"response.output_item.done","item":{"type":"function_call","arguments":"{invalid"}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","arguments":null}}`,
		`{"type":"response.output_item.done","item":{"type":"message","arguments":"{\"n\":1.0}"}}`,
		`{"type":"response.function_call_arguments.delta","delta":"1.0"}`,
	} {
		if got := normalizeMuseToolArguments([]byte(event), "muse-spark-1.3"); string(got) != event {
			t.Fatalf("modified unsupported event: %s", got)
		}
	}
	var state any
	event := []byte(`{"type":"response.output_item.done","item":{"id":"fc1","type":"function_call","arguments":"{\"n\":1.0}"}}`)
	got := ConvertCodexResponseToOpenAIResponses(context.Background(), "gpt-6-astra", nil, nil, event, &state)
	if len(got) != 1 || !bytes.Equal(got[0], event) || state != nil {
		t.Fatal("modified non-Muse response or stream state")
	}
}
