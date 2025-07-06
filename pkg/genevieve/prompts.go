package genevieve

import (
	"fmt"
	"strings"
)

func AgentSystemPrompt() string {
	return "You're an agent that chooses the right tool to answer a user's question."
}

func AgentChooseToolPrompt(toolNames []string, question string) string {
	return fmt.Sprintf(`Here are the available tools: %s.
Question: %s
Choose the most appropriate tool and the input to send to it.
Deliver the response in plain text without any Markdown or formatting.
Provide the output as raw text. This is the schema for your output: %s`,
		strings.Join(toolNames, ", "),
		question,
		AgentChooseToolSchema,
	)
}
