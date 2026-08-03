package service

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	agentModel "twitter-clone/internal/module/agent/model"
	agentRuntime "twitter-clone/internal/module/agent/runtime"
	platformTrace "twitter-clone/pkg/trace"

	"github.com/sashabaranov/go-openai"
)

const (
	providerDashScope = "dashscope"
	providerLMStudio  = "lmstudio"
)

func buildDefaultProviderRouter(
	premiumClient *openai.Client,
	premiumModel string,
) (*agentModel.ProviderRouter, error) {
	localModel := strings.TrimSpace(os.Getenv("LM_STUDIO_MODEL_CHAT"))
	if localModel == "" {
		localModel = "qwen2.5-3b-instruct"
	}

	definitions := []agentModel.Definition{{
		ID: premiumModel, Provider: providerDashScope,
		ContextWindow:   envPositiveInt("AGENT_DASHSCOPE_CONTEXT_WINDOW", 32768),
		MaxOutputTokens: envPositiveInt("AGENT_DASHSCOPE_MAX_OUTPUT_TOKENS", 8192),
		Pricing: agentModel.Pricing{
			InputMicrosPerMillionTokens:  envNonNegativeInt64("AGENT_DASHSCOPE_INPUT_MICROS_PER_MILLION", 0),
			OutputMicrosPerMillionTokens: envNonNegativeInt64("AGENT_DASHSCOPE_OUTPUT_MICROS_PER_MILLION", 0),
			Version:                      envString("AGENT_DASHSCOPE_PRICING_VERSION", "unpriced"),
		},
		Capabilities: []agentModel.Capability{
			agentModel.CapabilityChat, agentModel.CapabilityToolCall, agentModel.CapabilityJSON,
		},
	}}
	if localModel != premiumModel {
		definitions[0].Fallbacks = []string{localModel}
		definitions = append(definitions, agentModel.Definition{
			ID: localModel, Provider: providerLMStudio,
			ContextWindow:   envPositiveInt("AGENT_LMSTUDIO_CONTEXT_WINDOW", 32768),
			MaxOutputTokens: envPositiveInt("AGENT_LMSTUDIO_MAX_OUTPUT_TOKENS", 4096),
			Pricing: agentModel.Pricing{
				InputMicrosPerMillionTokens:  envNonNegativeInt64("AGENT_LMSTUDIO_INPUT_MICROS_PER_MILLION", 0),
				OutputMicrosPerMillionTokens: envNonNegativeInt64("AGENT_LMSTUDIO_OUTPUT_MICROS_PER_MILLION", 0),
				Version:                      envString("AGENT_LMSTUDIO_PRICING_VERSION", "local-v1"),
			},
			Capabilities: []agentModel.Capability{
				agentModel.CapabilityChat, agentModel.CapabilityToolCall, agentModel.CapabilityJSON,
			},
			Fallbacks: []string{premiumModel},
		})
	}
	catalog, err := agentModel.NewCatalog(definitions)
	if err != nil {
		return nil, err
	}

	providers := map[string]agentRuntime.ModelClient{
		providerDashScope: agentModel.NewOpenAICompatibleClient(premiumClient, premiumModel, providerDashScope),
	}
	if localModel != premiumModel {
		baseURL := envString("LM_STUDIO_API_URL", "http://localhost:1234/v1")
		policyHosts := strings.Split(os.Getenv("AGENT_LLM_ALLOWED_HOSTS"), ",")
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil {
			policyHosts = append(policyHosts, parsed.Hostname())
		}
		policy := agentModel.NewEndpointPolicy(policyHosts...)
		config := openai.DefaultConfig("lm-studio")
		config.BaseURL = baseURL
		config.HTTPClient = platformTrace.InstrumentHTTPClient(
			agentModel.NewRestrictedHTTPClient(policy, providerLMStudio),
			"agent.provider.http",
			nil,
		)
		localClient := openai.NewClientWithConfig(config)
		providers[providerLMStudio] = agentModel.NewOpenAICompatibleClient(localClient, localModel, providerLMStudio)
	}
	return agentModel.NewProviderRouter(catalog, providers)
}

func envPositiveInt(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envNonNegativeInt64(name string, fallback int64) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
