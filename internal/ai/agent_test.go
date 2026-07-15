package ai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func cacheBreakpoints(t *testing.T, messages []anthropic.MessageParam) int {
	t.Helper()
	data, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return strings.Count(string(data), "cache_control")
}

func sampleConversation() []anthropic.MessageParam {
	return []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("hello there")),
		anthropic.NewUserMessage(anthropic.NewTextBlock("make a playlist")),
		anthropic.NewAssistantMessage(anthropic.NewTextBlock("done")),
	}
}

func TestMarkConversationCacheSingleBreakpoint(t *testing.T) {
	msgs := sampleConversation()
	markConversationCache(msgs)
	if got := cacheBreakpoints(t, msgs); got != 1 {
		t.Fatalf("after first mark: %d breakpoints, want 1", got)
	}
	// Marking again must not accumulate breakpoints (Anthropic caps at 4).
	markConversationCache(msgs)
	if got := cacheBreakpoints(t, msgs); got != 1 {
		t.Fatalf("after second mark: %d breakpoints, want 1", got)
	}
	// The breakpoint must be on the final block.
	data, _ := json.Marshal(msgs[len(msgs)-1])
	if !strings.Contains(string(data), "cache_control") {
		t.Error("breakpoint should be on the last message")
	}
}

func TestStripCacheClearsAll(t *testing.T) {
	a := &Agent{}
	msgs := sampleConversation()
	markConversationCache(msgs)
	a.stripCache(msgs)
	if got := cacheBreakpoints(t, msgs); got != 0 {
		t.Fatalf("stripCache left %d breakpoints, want 0", got)
	}
}

func TestIsContextLengthError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("prompt is too long: 200027 tokens > 200000 maximum"), true},
		{errors.New("some other error"), false},
	}
	for _, c := range cases {
		if got := isContextLengthError(c.err); got != c.want {
			t.Errorf("isContextLengthError(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestEstimateTokensGrowsWithSize(t *testing.T) {
	small := estimateMessagesTokens([]anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock("hi")),
	})
	big := estimateMessagesTokens([]anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(strings.Repeat("word ", 5000))),
	})
	if big <= small {
		t.Errorf("expected larger message to estimate more tokens: small=%d big=%d", small, big)
	}
}
