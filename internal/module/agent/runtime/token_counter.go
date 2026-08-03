package runtime

const messageTokenOverhead = 4

type HeuristicTokenCounter struct{}

func NewHeuristicTokenCounter() *HeuristicTokenCounter {
	return &HeuristicTokenCounter{}
}

func (c *HeuristicTokenCounter) CountText(text string) int {
	if text == "" {
		return 0
	}
	ascii := 0
	nonASCII := 0
	for _, char := range text {
		if char <= 127 {
			ascii++
		} else {
			nonASCII++
		}
	}
	return (ascii+3)/4 + nonASCII
}

func (c *HeuristicTokenCounter) CountMessages(messages []Message) int {
	total := 0
	for _, message := range messages {
		total += messageTokenOverhead
		total += c.CountText(string(message.Role))
		total += c.CountText(message.Name)
		total += c.CountText(message.ToolCallID)
		total += c.CountText(message.Content)
		for _, action := range message.Actions {
			total += 4 + c.CountText(action.ID) + c.CountText(string(action.Type))
			total += c.CountText(action.Name) + c.CountText(action.Content)
			total += c.CountText(string(action.Arguments))
		}
	}
	return total
}

func (c *HeuristicTokenCounter) EstimateRequest(request ModelRequest) TokenUsage {
	input := c.CountMessages(request.Messages)
	for _, tool := range request.Tools {
		input += 8 + c.CountText(tool.Name) + c.CountText(tool.Description)
		input += c.CountText(string(tool.InputSchema))
	}
	return TokenUsage{InputTokens: input, TotalTokens: input, Estimated: true}
}

func (c *HeuristicTokenCounter) EstimateResponse(response ModelResponse) TokenUsage {
	message := response.Message
	if len(message.Actions) == 0 && len(response.Actions) > 0 {
		message.Actions = response.Actions
	}
	output := c.CountMessages([]Message{message})
	return TokenUsage{OutputTokens: output, TotalTokens: output, Estimated: true}
}
