package main

// Prompt construction, kept out of main.go so the topology there stays legible.
// These are ordinary typed Go functions over State, checked at compile time; a
// caller who wants templating can call text/template here instead.

import (
	"fmt"
	"strings"
)

// publishCriteria is the shared bar. Both prompts reference it so the critic
// judges against the same list the reviser is asked to satisfy — an unstated
// bar is why "is this good enough?" gets answered yes on the first pass.
const publishCriteria = `A publishable paragraph must satisfy ALL of:
  1. It names a concrete, specific mechanism — not a general claim.
  2. It includes at least one concrete example or number.
  3. It states the tradeoff or cost of the thing it advocates.
  4. It contains no hedging filler ("often", "can be", "it depends", "in many cases").
  5. It is under 120 words.`

// draftPrompt asks for a deliberately quick first pass, then for targeted
// revisions once a critique exists. The rough first draft is what makes the
// loop worth demonstrating: an already-polished draft gets approved on step one
// and the topology never loops.
func draftPrompt(s State) string {
	var b strings.Builder
	if last := s.lastCritique(); last != "" {
		fmt.Fprintf(&b, "Revise this paragraph about: %s.\n\n", s.Topic)
		fmt.Fprintf(&b, "Previous draft:\n%s\n\n", s.Draft)
		fmt.Fprintf(&b, "Critique to address:\n%s\n\n", last)
		b.WriteString("Fix every issue the critique raises. Output only the revised paragraph.\n\n")
		b.WriteString(publishCriteria)
		return b.String()
	}
	fmt.Fprintf(&b, "Write a quick first-pass paragraph about: %s.\n", s.Topic)
	b.WriteString("This is a rough draft, not a final one. Do not polish it, do not " +
		"add examples or numbers, and keep it general. Output only the paragraph.\n")
	return b.String()
}

// critiquePrompt constrains the reply to a routable shape: the critique router
// keys off the leading APPROVE / REVISE token. The criteria are spelled out so
// the verdict is a check against a list rather than a matter of taste.
func critiquePrompt(s State) string {
	var b strings.Builder
	b.WriteString("You are a strict editor. Judge the paragraph below against the criteria.\n\n")
	b.WriteString(publishCriteria)
	b.WriteString("\n\nIf every criterion is met, reply with the single word APPROVE and nothing else.\n")
	b.WriteString("If any criterion fails, reply beginning with the word REVISE, then name " +
		"the failing criteria by number with one concrete fix each.\n")
	b.WriteString("Do not approve a paragraph that merely reads well; it must meet every criterion.\n\n")
	fmt.Fprintf(&b, "Paragraph:\n%s", s.Draft)
	return b.String()
}

// approved reports whether a critique cleared the draft for publication.
func approved(critique string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(critique)), "APPROVE")
}
