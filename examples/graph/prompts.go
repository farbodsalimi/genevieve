package main

import (
	"fmt"
	"strings"
)

const reportCriteria = `The report must:
1. answer the user's request directly;
2. distinguish evidence from inference;
3. represent conflicting specialist conclusions;
4. disclose failed or missing specialist perspectives;
5. include concrete risks and limitations.`

func intakePrompt(s State) string {
	return fmt.Sprintf(`Act as an intake gate. Decide whether this request is specific,
safe, and answerable as a short research brief.

Reply on one line as either:
ACCEPT: <brief reason>
REJECT: <brief reason and what the user should clarify>

Request: %s`, s.Query)
}

func parseIntake(text string) Update {
	trimmed := strings.TrimSpace(text)
	accepted := strings.HasPrefix(strings.ToUpper(trimmed), "ACCEPT:")
	return Update{Accepted: new(accepted), IntakeNote: trimmed}
}

func specialistPrompt(role string, s State) string {
	return fmt.Sprintf(`You are the %s in a parallel research team.
Investigate the request from only your assigned perspective. Use the evidence_lookup
tool before answering. Cite evidence IDs returned by the tool, distinguish facts from
inference, and state uncertainty. Return a compact specialist memo.

User request: %s`, role, s.Query)
}

func synthesisPrompt(s State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Synthesize a decision-ready research brief for this request:\n%s\n\n", s.Query)
	b.WriteString(reportCriteria)
	b.WriteString("\n\nSpecialist memos:\n")
	for _, report := range s.Reports {
		fmt.Fprintf(&b, "\n[%s]\n%s\n", report.Specialist, report.Report)
	}
	if len(s.Failures) > 0 {
		b.WriteString("\nFailed specialists (disclose these as limitations):\n")
		for _, failure := range s.Failures {
			fmt.Fprintf(&b, "- %s: %s\n", failure.Node, failure.Error)
		}
	}
	if s.Review != "" {
		fmt.Fprintf(&b, "\nPrevious draft:\n%s\n\nReviewer feedback:\n%s\n", s.DraftJSON, s.Review)
	}
	b.WriteString("\nYou must finish by calling report_observation with the complete structured report.")
	return b.String()
}

func reviewPrompt(s State) string {
	return fmt.Sprintf(`You are the quality gate. Evaluate the JSON report against every criterion.

%s

Reply with only APPROVE if all criteria pass. Otherwise start with REVISE: and give
specific corrections. Do not reject merely because a specialist failed if that failure
is disclosed honestly.

User request: %s
Report: %s`, reportCriteria, s.Query, s.DraftJSON)
}

func approved(review string) bool {
	return strings.EqualFold(strings.TrimSpace(review), "APPROVE")
}
