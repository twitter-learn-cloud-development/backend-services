package message

import (
	"fmt"
	"sort"
	"strings"

	agentRuntime "twitter-clone/internal/module/agent/runtime"
)

type BudgetedBuilder struct {
	counter    agentRuntime.TokenCounter
	compressor Compressor
}

func NewBuilder(counter agentRuntime.TokenCounter, compressor Compressor) *BudgetedBuilder {
	if counter == nil {
		counter = agentRuntime.NewHeuristicTokenCounter()
	}
	if compressor == nil {
		compressor = NewTokenAwareTruncator(counter)
	}
	return &BudgetedBuilder{counter: counter, compressor: compressor}
}

func (b *BudgetedBuilder) Build(request BuildRequest) (BuildResult, error) {
	result := BuildResult{Dropped: make(map[Source]int)}
	if err := validateMandatoryMessages(request); err != nil {
		return result, err
	}

	header := make([]agentRuntime.Message, 0, len(request.System)+len(request.Developer)+len(request.Policy))
	header = append(header, cloneMessages(request.System)...)
	header = append(header, cloneMessages(request.Developer)...)
	header = append(header, cloneMessages(request.Policy)...)
	mandatory := append(cloneMessages(header), cloneMessage(request.Current))
	used := b.counter.CountMessages(mandatory)
	if request.Budget.MaxInputTokens > 0 && used > request.Budget.MaxInputTokens {
		return result, fmt.Errorf("%w: required %d, limit %d", ErrMandatoryContextTooLarge, used, request.Budget.MaxInputTokens)
	}

	units, malformedHistory, malformedTools := splitHistory(request.History)
	result.Dropped[SourceHistory] += malformedHistory
	result.Dropped[SourceToolResult] += malformedTools
	selected, historyUsed, toolUsed := b.selectHistory(units, request.Budget, used, result.Dropped)
	used += historyUsed + toolUsed

	contextMessages := make([]agentRuntime.Message, 0)
	memoryUsed := 0
	contextMessages, used, memoryUsed = b.selectFragments(contextMessages, request.Persona, SourcePersona, request.Budget.MemoryTokens, memoryUsed, request.Budget.MaxInputTokens, used, result.Dropped)
	contextMessages, used, _ = b.selectFragments(contextMessages, request.Memory, SourceMemory, request.Budget.MemoryTokens, memoryUsed, request.Budget.MaxInputTokens, used, result.Dropped)
	contextMessages, used, _ = b.selectFragments(contextMessages, request.RAG, SourceRAG, request.Budget.RAGTokens, 0, request.Budget.MaxInputTokens, used, result.Dropped)
	contextMessages, used, _ = b.selectFragments(contextMessages, request.Blackboard, SourceBlackboard, request.Budget.BlackboardTokens, 0, request.Budget.MaxInputTokens, used, result.Dropped)

	result.Messages = append(result.Messages, header...)
	result.Messages = append(result.Messages, contextMessages...)
	result.Messages = append(result.Messages, selected...)
	result.Messages = append(result.Messages, cloneMessage(request.Current))
	result.EstimatedTokens = b.counter.CountMessages(result.Messages)
	return result, nil
}

type historyUnit struct {
	messages []agentRuntime.Message
	tool     bool
}

func (b *BudgetedBuilder) selectHistory(
	units []historyUnit,
	budget Budget,
	initialUsed int,
	dropped map[Source]int,
) ([]agentRuntime.Message, int, int) {
	selected := make(map[int][]agentRuntime.Message, len(units))
	historyUsed := 0
	toolUsed := 0
	totalUsed := initialUsed

	for _, wantTool := range []bool{true, false} {
		for index := len(units) - 1; index >= 0; index-- {
			unit := units[index]
			if unit.tool != wantTool {
				continue
			}
			bucketLimit := budget.HistoryTokens
			bucketUsed := historyUsed
			source := SourceHistory
			if unit.tool {
				bucketLimit = budget.ToolResultTokens
				bucketUsed = toolUsed
				source = SourceToolResult
			}
			available := minPositive(remaining(bucketLimit, bucketUsed), remaining(budget.MaxInputTokens, totalUsed))
			candidate := cloneMessages(unit.messages)
			tokens := b.counter.CountMessages(candidate)
			if available >= 0 && tokens > available {
				candidate = b.compressHistoryUnit(unit, available)
				tokens = b.counter.CountMessages(candidate)
			}
			if len(candidate) == 0 || (available >= 0 && tokens > available) {
				dropped[source] += len(unit.messages)
				continue
			}
			selected[index] = candidate
			totalUsed += tokens
			if unit.tool {
				toolUsed += tokens
			} else {
				historyUsed += tokens
			}
		}
	}

	indices := make([]int, 0, len(selected))
	for index := range selected {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	messages := make([]agentRuntime.Message, 0)
	for _, index := range indices {
		messages = append(messages, selected[index]...)
	}
	return messages, historyUsed, toolUsed
}

func (b *BudgetedBuilder) compressHistoryUnit(unit historyUnit, maxTokens int) []agentRuntime.Message {
	if maxTokens <= 0 {
		return nil
	}
	candidate := cloneMessages(unit.messages)
	compressible := make([]int, 0, len(candidate))
	for index := range candidate {
		if unit.tool && candidate[index].Role != agentRuntime.RoleTool {
			continue
		}
		if strings.TrimSpace(candidate[index].Content) != "" {
			compressible = append(compressible, index)
			candidate[index].Content = ""
		}
	}
	if len(compressible) == 0 {
		return nil
	}
	fixedTokens := b.counter.CountMessages(candidate)
	if fixedTokens >= maxTokens {
		return nil
	}
	contentBudget := (maxTokens - fixedTokens) / len(compressible)
	for _, index := range compressible {
		candidate[index].Content = b.compressor.Compress(unit.messages[index].Content, contentBudget)
	}
	return candidate
}

func (b *BudgetedBuilder) selectFragments(
	selected []agentRuntime.Message,
	fragments []Fragment,
	defaultSource Source,
	bucketLimit int,
	bucketUsed int,
	maxInput int,
	totalUsed int,
	dropped map[Source]int,
) ([]agentRuntime.Message, int, int) {
	fragments = append([]Fragment(nil), fragments...)
	sort.SliceStable(fragments, func(i, j int) bool { return fragments[i].Score > fragments[j].Score })
	for _, fragment := range fragments {
		source := fragment.Source
		if source == "" {
			source = defaultSource
		}
		if strings.TrimSpace(fragment.Content) == "" {
			dropped[source]++
			continue
		}
		message := fragmentMessage(source, fragment)
		tokens := b.counter.CountMessages([]agentRuntime.Message{message})
		available := minPositive(remaining(bucketLimit, bucketUsed), remaining(maxInput, totalUsed))
		// Context fragments, especially RAG chunks, are atomic by design.
		if available >= 0 && tokens > available {
			dropped[source]++
			continue
		}
		selected = append(selected, message)
		bucketUsed += tokens
		totalUsed += tokens
	}
	return selected, totalUsed, bucketUsed
}

func validateMandatoryMessages(request BuildRequest) error {
	for _, message := range request.System {
		if message.Role != agentRuntime.RoleSystem {
			return fmt.Errorf("system message has role %q", message.Role)
		}
	}
	for _, message := range request.Developer {
		if message.Role != agentRuntime.RoleDeveloper {
			return fmt.Errorf("developer message has role %q", message.Role)
		}
	}
	for _, message := range request.Policy {
		if message.Role != agentRuntime.RoleSystem && message.Role != agentRuntime.RoleDeveloper {
			return fmt.Errorf("policy message has role %q", message.Role)
		}
	}
	if request.Current.Role != agentRuntime.RoleUser || strings.TrimSpace(request.Current.Content) == "" {
		return fmt.Errorf("current input must be a non-empty user message")
	}
	return nil
}

func splitHistory(messages []agentRuntime.Message) ([]historyUnit, int, int) {
	units := make([]historyUnit, 0, len(messages))
	malformedHistory := 0
	malformedTools := 0
	for index := 0; index < len(messages); {
		message := messages[index]
		if message.Role == agentRuntime.RoleTool {
			malformedTools++
			index++
			continue
		}
		expected := expectedToolResults(message)
		if len(expected) == 0 {
			units = append(units, historyUnit{messages: []agentRuntime.Message{cloneMessage(message)}})
			index++
			continue
		}

		end := index + 1
		found := make(map[string]struct{}, len(expected))
		valid := true
		for end < len(messages) && messages[end].Role == agentRuntime.RoleTool {
			toolMessage := messages[end]
			if _, ok := expected[toolMessage.ToolCallID]; !ok {
				valid = false
			}
			if _, duplicate := found[toolMessage.ToolCallID]; duplicate {
				valid = false
			}
			found[toolMessage.ToolCallID] = struct{}{}
			end++
		}
		if len(found) != len(expected) {
			valid = false
		}
		if !valid {
			malformedHistory++
			malformedTools += end - index - 1
			index = end
			continue
		}
		units = append(units, historyUnit{messages: cloneMessages(messages[index:end]), tool: true})
		index = end
	}
	return units, malformedHistory, malformedTools
}

func expectedToolResults(message agentRuntime.Message) map[string]struct{} {
	if message.Role != agentRuntime.RoleAssistant {
		return nil
	}
	expected := make(map[string]struct{})
	for _, action := range message.Actions {
		if action.Type != agentRuntime.ActionToolCall && action.Type != agentRuntime.ActionRAGSearch {
			continue
		}
		if strings.TrimSpace(action.ID) == "" {
			return map[string]struct{}{"": {}}
		}
		expected[action.ID] = struct{}{}
	}
	return expected
}

func fragmentMessage(source Source, fragment Fragment) agentRuntime.Message {
	name := strings.TrimSpace(fragment.Name)
	if name == "" {
		name = string(source)
	}
	return agentRuntime.Message{
		Role:    agentRuntime.RoleSystem,
		Name:    name,
		Content: fmt.Sprintf("[%s]\n%s", source, strings.TrimSpace(fragment.Content)),
	}
}

func remaining(limit, used int) int {
	if limit <= 0 {
		return -1
	}
	if used >= limit {
		return 0
	}
	return limit - used
}

func minPositive(left, right int) int {
	if left < 0 {
		return right
	}
	if right < 0 {
		return left
	}
	if left < right {
		return left
	}
	return right
}

func cloneMessages(messages []agentRuntime.Message) []agentRuntime.Message {
	cloned := make([]agentRuntime.Message, len(messages))
	for index, message := range messages {
		cloned[index] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message agentRuntime.Message) agentRuntime.Message {
	message.Actions = append([]agentRuntime.Action(nil), message.Actions...)
	for index := range message.Actions {
		message.Actions[index].Arguments = append([]byte(nil), message.Actions[index].Arguments...)
	}
	return message
}
