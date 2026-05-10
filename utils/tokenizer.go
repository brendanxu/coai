package utils

import (
	"chat/globals"
	"fmt"
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

//   Using https://github.com/pkoukk/tiktoken-go
//   To count number of tokens of openai chat messages
//   OpenAI Cookbook: https://github.com/openai/openai-cookbook/blob/main/examples/How_to_count_tokens_with_tiktoken.ipynb

func GetWeightByModel(model string) int {
	switch model {
	case globals.GPT3TurboInstruct,
		globals.Claude1, globals.Claude1100k,
		globals.Claude2, globals.Claude2100k, globals.Claude2200k:
		return 2
	case globals.GPT3Turbo, globals.GPT3Turbo0613, globals.GPT3Turbo1106, globals.GPT3Turbo0125,
		globals.GPT3Turbo16k, globals.GPT3Turbo16k0613,
		globals.GPT4, globals.GPT40314, globals.GPT40613,
		globals.GPT41106Preview, globals.GPT4TurboPreview, globals.GPT40125Preview,
		globals.GPT4VisionPreview, globals.GPT41106VisionPreview,
		globals.GPT432k, globals.GPT432k0613, globals.GPT432k0314:
		return 3
	case globals.GPT3Turbo0301, globals.GPT3Turbo16k0301:
		return 4 // every message follows <|start|>{role/name}\n{content}<|end|>\n
	default:
		if strings.Contains(model, globals.GPT3Turbo) {
			// warning: gpt-3.5-turbo may update over time. Returning num tokens assuming gpt-3.5-turbo-0613.
			return GetWeightByModel(globals.GPT3Turbo0613)
		} else if strings.Contains(model, globals.GPT4) {
			// warning: gpt-4 may update over time. Returning num tokens assuming gpt-4-0613.
			return GetWeightByModel(globals.GPT40613)
		} else if strings.Contains(model, globals.Claude1) {
			// warning: claude-1 may update over time. Returning num tokens assuming claude-1-100k.
			return GetWeightByModel(globals.Claude1100k)
		} else if strings.Contains(model, globals.Claude2) {
			// warning: claude-2 may update over time. Returning num tokens assuming claude-2-100k.
			return GetWeightByModel(globals.Claude2100k)
		} else {
			// not implemented: See https://github.com/openai/openai-python/blob/main/chatml.md for information on how messages are converted to tokens
			return 3
		}
	}
}
func NumTokensFromMessages(messages []globals.Message, model string, responseType bool) (tokens int) {
	tokensPerMessage := GetWeightByModel(model)
	tkm, err := tiktoken.EncodingForModel(model)

	if err != nil {
		// the method above was deprecated, use the recall method instead
		// can not encode messages, use length of messages as a proxy for number of tokens
		// using rune instead of byte to account for unicode characters (e.g. emojis, non-english characters)
		// data := Marshal(messages)
		// return len([]rune(data)) * weight

		// use the recall method instead (default encoder model is gpt-3.5-turbo-0613)
		if globals.DebugMode {
			globals.Debug(fmt.Sprintf("[tiktoken] error encoding messages: %s (model: %s), using default model instead", err, model))
		}
		return NumTokensFromMessages(messages, globals.GPT3Turbo0613, responseType)
	}

	for _, message := range messages {
		tokens += len(tkm.Encode(message.Content.String(), nil, nil))

		if !responseType {
			tokens += len(tkm.Encode(message.Role, nil, nil)) + tokensPerMessage
		}
	}

	if !responseType {
		tokens += 3 // every reply is primed with <|start|>assistant<|message|>
	}

	if globals.DebugMode {
		globals.Debug(fmt.Sprintf("[tiktoken] num tokens from messages: %d (tokens per message: %d, model: %s)", tokens, tokensPerMessage, model))
	}
	return tokens
}

func NumTokensFromResponse(response string, model string) int {
	if len(response) == 0 {
		return 0
	}

	return NumTokensFromMessages([]globals.Message{{Content: globals.MessageContent{Plain: response}}}, model, true)
}

func CountInputQuota(charge Charge, token int) float32 {
	if charge.GetType() == globals.TokenBilling {
		return float32(token) / 1000 * charge.GetInput()
	}

	return 0
}

func CountOutputToken(charge Charge, token int) float32 {
	switch charge.GetType() {
	case globals.TokenBilling:
		return float32(token) / 1000 * charge.GetOutput()
	case globals.TimesBilling:
		return charge.GetOutput()
	default:
		return 0
	}
}

// CountUpstreamQuota prices a provider-truth UpstreamUsage block against
// a Charge config. Each token class is billed independently:
//
//	un-cached input → charge.GetInput()
//	output          → charge.GetOutput()  (or fixed times-billing fee)
//	cache_read      → charge.GetCacheRead()
//	cache_write     → charge.GetCacheWrite5m() / GetCacheWrite1h()
//	                  by usage.CacheTTL ('1h' uses the 1h rate, anything
//	                  else uses the 5m rate to be conservative)
//
// This implements the "we never lose money" rule
// (docs/research/token-cache-AUDIT-and-billing-design.md §2.4): every
// class's customer rate ≥ upstream rate × 1.0, so total profit is
// always ≥ 0 regardless of how the customer splits cache vs un-cached.
//
// times-billing models bill a flat per-call fee; this helper returns
// charge.GetOutput() for them and ignores the token counts (cache
// breakdown is irrelevant to a flat-rate billing tier).
//
// Returns 0 when usage is nil so callers can defensively chain it.
func CountUpstreamQuota(charge Charge, usage *globals.UpstreamUsage) float32 {
	if usage == nil || charge == nil {
		return 0
	}
	switch charge.GetType() {
	case globals.NonBilling:
		return 0
	case globals.TimesBilling:
		return charge.GetOutput()
	case globals.TokenBilling:
		// fall through
	default:
		return 0
	}

	cacheWriteRate := charge.GetCacheWrite5m()
	if usage.CacheTTL == "1h" {
		cacheWriteRate = charge.GetCacheWrite1h()
	}

	total := float32(usage.InputTokens)/1000*charge.GetInput() +
		float32(usage.OutputTokens)/1000*charge.GetOutput() +
		float32(usage.CacheReadTokens)/1000*charge.GetCacheRead() +
		float32(usage.CacheWriteTokens)/1000*cacheWriteRate

	if total < 0 {
		return 0
	}
	return total
}
