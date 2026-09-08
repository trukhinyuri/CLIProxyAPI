package responses

import (
	"bytes"
	"context"
	"strings"
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
		{`{"a":[10e-1,1.230e2,1.230e1,-12.0,-0.0,1000e-4]}`, `{"a":[1,123,1.230e1,-12,0,1000e-4]}`},
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

func TestMuseArgumentsBoundNumericExpansion(t *testing.T) {
	for _, raw := range []string{
		`{"n":1e100000}`,
		`{"n":1e-100000}`,
		`{"n":1e999999999999999999999}`,
		`{"n":1e-999999999999999999999}`,
		`{"n":` + strings.Repeat("9", maxMuseNumberDigits+1) + `.0}`,
	} {
		p, err := sjson.Set(`{"type":"response.function_call_arguments.done"}`, "arguments", raw)
		if err != nil {
			t.Fatal(err)
		}
		got := normalizeMuseToolArguments([]byte(p), "muse-spark-1.3")
		if gjson.GetBytes(got, "arguments").String() != raw {
			t.Fatalf("expanded oversized number (input length %d, output length %d)", len(raw), len(got))
		}
	}
}

func TestMuseArgumentsBoundCumulativeExpansion(t *testing.T) {
	// Each individual number is below the limit, but their combined expansion
	// exceeds the argument budget. Preserve the original string in full.
	raw := `{"timeout_ms":120000.0,"values":[` + strings.Repeat("1e1000,", 79) + `1e1000]}`
	p, err := sjson.Set(`{"type":"response.output_item.done","item":{"type":"function_call"}}`, "item.arguments", raw)
	if err != nil {
		t.Fatal(err)
	}
	got := normalizeMuseToolArguments([]byte(p), "muse-spark-1.3")
	if string(got) != p {
		t.Fatalf("did not preserve original arguments when expansion budget was exceeded (output length %d)", len(got))
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

func BenchmarkMuseFragmentedArguments(b *testing.B) {
	raw := `{"message":"` + strings.Repeat("x", 256*1024) + `"}`
	var events [][]byte
	for offset := 0; offset < len(raw); offset += 16 {
		end := min(offset+16, len(raw))
		event, err := sjson.SetBytes([]byte(`{"type":"response.function_call_arguments.delta","item_id":"fc1"}`), "delta", raw[offset:end])
		if err != nil {
			b.Fatal(err)
		}
		events = append(events, event)
	}
	done := []byte(`{"type":"response.function_call_arguments.done","item_id":"fc1","arguments":""}`)
	final := []byte(`{"type":"response.output_item.done","item":{"id":"fc1","type":"function_call","arguments":""}}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var state any
		for _, event := range events {
			restoreMuseArguments(event, "muse-spark-1.3", &state)
		}
		restoreMuseArguments(done, "muse-spark-1.3", &state)
		got := restoreMuseArguments(final, "muse-spark-1.3", &state)
		if gjson.GetBytes(got, "item.arguments").String() != raw {
			b.Fatal("lost fragmented arguments")
		}
	}
}
